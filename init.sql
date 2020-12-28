CREATE DATABASE IF NOT EXISTS hellomemsql;
CREATE TABLE IF NOT EXISTS hellomemsql.test (message text NOT NULL);

CREATE DATABASE IF NOT EXISTS hellomemsql1;
CREATE TABLE IF NOT EXISTS hellomemsql1.test (message text NOT NULL);

CREATE DATABASE IF NOT EXISTS hellomemsql2;
CREATE TABLE IF NOT EXISTS hellomemsql2.test (message text NOT NULL);

create user 'user1'@'%' identified by 'pass1';
create user 'user2'@'%' identified by 'pass2';
create user 'user3'@'%' identified by 'pass3';
GRANT ALL PRIVILEGES ON hellomemsql1.* TO 'user1'@'%' WITH GRANT OPTION;
GRANT ALL PRIVILEGES ON hellomemsql1.* TO 'user2'@'%' WITH GRANT OPTION;
GRANT ALL PRIVILEGES ON hellomemsql1.* TO 'user3'@'%' WITH GRANT OPTION;


create user 'user4'@'%' identified by 'pass4';
create user 'user5'@'%' identified by 'pass5';
create user 'user6'@'%' identified by 'pass6';
GRANT ALL PRIVILEGES ON hellomemsql2.* TO 'user4'@'%' WITH GRANT OPTION;
GRANT ALL PRIVILEGES ON hellomemsql2.* TO 'user5'@'%' WITH GRANT OPTION;
GRANT ALL PRIVILEGES ON hellomemsql2.* TO 'user6'@'%' WITH GRANT OPTION;

FLUSH PRIVILEGES;

