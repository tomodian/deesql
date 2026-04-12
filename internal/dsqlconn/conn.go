package dsqlconn

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/tomodian/deesql/internal/ui"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/feature/dsql/auth"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/go-playground/validator/v10"
	"github.com/jackc/pgconn"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/stdlib"
)

var validate = validator.New()

type ConnectInput struct {
	Endpoint string `validate:"required"`
	Region   string
	User     string `validate:"required"`

	// AWS profile name (optional). Uses the named profile from ~/.aws/config.
	Profile string

	// IAM Role ARN to assume (optional).
	RoleARN string

	// Connection timeout (default 10s).
	ConnectTimeout time.Duration
}

type ConnectOutput struct {
	DB *sql.DB
}

var regionRe = regexp.MustCompile(`\.dsql\.([a-z0-9-]+)\.on\.aws$`)

func ParseRegion(endpoint string) (string, error) {
	m := regionRe.FindStringSubmatch(endpoint)
	if m == nil {
		return "", fmt.Errorf("cannot detect region from endpoint %q; use --region", endpoint)
	}
	return m[1], nil
}

func Connect(ctx context.Context, in ConnectInput) (*ConnectOutput, error) {
	if err := validate.Struct(in); err != nil {
		return nil, fmt.Errorf("invalid connect input: %w", err)
	}

	// Password auth mode: skip IAM, TLS, and region detection.
	if pgUser := os.Getenv("POSTGRES_USER"); pgUser != "" {
		return connectWithPassword(ctx, in, pgUser)
	}

	ui.Step("Connecting to %s as %s", in.Endpoint, in.User)

	region := in.Region
	if region == "" {
		var err error
		region, err = ParseRegion(in.Endpoint)
		if err != nil {
			return nil, err
		}
		ui.Dim("    Region: %s (auto-detected)\n", region)
	} else {
		ui.Dim("    Region: %s\n", region)
	}

	credsProvider, err := loadCredentials(ctx, in, region)
	if err != nil {
		return nil, err
	}

	creds, err := credsProvider.Retrieve(ctx)
	if err != nil {
		return nil, fmt.Errorf("retrieving AWS credentials: %w", err)
	}
	ui.Dim("    Credentials: %s...\n", creds.AccessKeyID[:4])

	ui.Step("Generating IAM auth token...")
	var token string
	if in.User == "admin" {
		token, err = auth.GenerateDBConnectAdminAuthToken(ctx, in.Endpoint, region, credsProvider)
	} else {
		token, err = auth.GenerateDbConnectAuthToken(ctx, in.Endpoint, region, credsProvider)
	}
	if err != nil {
		return nil, fmt.Errorf("generating IAM auth token: %w", err)
	}
	ui.Dim("    Token: %d chars\n", len(token))

	connCfg, err := pgx.ParseConfig("")
	if err != nil {
		return nil, fmt.Errorf("parsing pgx config: %w", err)
	}
	timeout := in.ConnectTimeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	tlsCfg := &tls.Config{
		ServerName: in.Endpoint,
		MinVersion: tls.VersionTLS12,
	}

	ips, err := net.LookupHost(in.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("resolving endpoint %s: %w", in.Endpoint, err)
	}
	ui.Dim("    Resolved: %v\n", ips)

	connCfg.Host = ips[0]
	connCfg.Port = 5432
	connCfg.User = in.User
	connCfg.Password = token
	connCfg.Database = "postgres"
	connCfg.ConnectTimeout = timeout
	connCfg.TLSConfig = tlsCfg

	connCfg.Fallbacks = make([]*pgconn.FallbackConfig, 0, len(ips)-1)
	for _, ip := range ips[1:] {
		connCfg.Fallbacks = append(connCfg.Fallbacks, &pgconn.FallbackConfig{
			Host:      ip,
			Port:      5432,
			TLSConfig: tlsCfg,
		})
	}

	ui.Step("Opening connection (SSL, timeout %s)...", timeout)
	db := stdlib.OpenDB(*connCfg)
	db.SetMaxOpenConns(1)

	totalTimeout := timeout * time.Duration(len(ips))
	connCtx, cancel := context.WithTimeout(ctx, totalTimeout)
	defer cancel()

	var one int
	if err := db.QueryRowContext(connCtx, "SELECT 1").Scan(&one); err != nil {
		db.Close()
		return nil, fmt.Errorf("connecting to DSQL (timeout %s): %w", timeout, err)
	}
	ui.Success("Connected to %s", in.Endpoint)

	return &ConnectOutput{DB: db}, nil
}

func connectWithPassword(ctx context.Context, in ConnectInput, pgUser string) (*ConnectOutput, error) {
	pgPass := os.Getenv("POSTGRES_PASSWORD")
	ui.Info("Auth bypass enabled (POSTGRES_USER=%s)", pgUser)

	host, portStr, err := net.SplitHostPort(in.Endpoint)
	if err != nil {
		// No port specified, use endpoint as host with default port.
		host = in.Endpoint
		portStr = "5432"
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return nil, fmt.Errorf("invalid port in endpoint %q: %w", in.Endpoint, err)
	}

	connCfg, err := pgx.ParseConfig("")
	if err != nil {
		return nil, fmt.Errorf("parsing pgx config: %w", err)
	}

	timeout := in.ConnectTimeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	connCfg.Host = host
	connCfg.Port = uint16(port)
	connCfg.User = pgUser
	connCfg.Password = pgPass
	connCfg.Database = "postgres"
	connCfg.ConnectTimeout = timeout
	connCfg.TLSConfig = nil

	ui.Step("Connecting to %s:%d as %s (password auth)...", host, port, pgUser)
	db := stdlib.OpenDB(*connCfg)
	db.SetMaxOpenConns(1)

	connCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var one int
	if err := db.QueryRowContext(connCtx, "SELECT 1").Scan(&one); err != nil {
		db.Close()
		return nil, fmt.Errorf("connecting to %s (timeout %s): %w", in.Endpoint, timeout, err)
	}
	ui.Success("Connected to %s", in.Endpoint)

	return &ConnectOutput{DB: db}, nil
}

func loadCredentials(ctx context.Context, in ConnectInput, region string) (aws.CredentialsProvider, error) {
	var loadOpts []func(*config.LoadOptions) error
	loadOpts = append(loadOpts, config.WithRegion(region))
	if in.Profile != "" {
		ui.Dim("    Profile: %s\n", in.Profile)
		loadOpts = append(loadOpts, config.WithSharedConfigProfile(in.Profile))
	}
	awsCfg, err := config.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}

	if in.RoleARN != "" {
		ui.Dim("    Assuming role: %s\n", in.RoleARN)
		stsClient := sts.NewFromConfig(awsCfg)
		return stscreds.NewAssumeRoleProvider(stsClient, in.RoleARN), nil
	}

	return awsCfg.Credentials, nil
}
