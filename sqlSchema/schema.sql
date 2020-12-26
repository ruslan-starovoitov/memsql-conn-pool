CREATE DATABASE hellomemsql1;
USE hellomemsql1;
CREATE TABLE IF NOT EXISTS test (
    message text NOT NULL
);

CREATE DATABASE hellomemsql2;
USE hellomemsql2;
CREATE TABLE IF NOT EXISTS test (
    message text NOT NULL
);

insert into test (message) value ("some message");
