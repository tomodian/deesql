-- Functions: only LANGUAGE SQL is supported on Aurora DSQL.

CREATE FUNCTION full_name(first_name TEXT, last_name TEXT) RETURNS TEXT
    LANGUAGE SQL
    AS 'SELECT first_name || '' '' || last_name';

CREATE FUNCTION is_overdue(due DATE) RETURNS BOOLEAN
    LANGUAGE SQL
    AS 'SELECT due < CURRENT_DATE';
