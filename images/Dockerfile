FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /dsql-migrate .

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /dsql-migrate /usr/local/bin/dsql-migrate

ENTRYPOINT ["dsql-migrate"]
