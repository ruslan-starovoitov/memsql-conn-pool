CREATE DATABASE IF NOT EXISTS hellomemsql;
USE hellomemsql;
CREATE TABLE IF NOT EXISTS test (
    message text NOT NULL
);

CREATE DATABASE IF NOT EXISTS hellomemsql6;
USE hellomemsql6;
CREATE TABLE IF NOT EXISTS test (
    message text NOT NULL
);
    create user 'user6'@'localhost' identified by 'pass6';
GRANT ALL PRIVILEGES ON *.* TO 'user6'@'localhost' WITH GRANT OPTION;

CREATE DATABASE IF NOT EXISTS hellomemsql7;
USE hellomemsql7;
CREATE TABLE IF NOT EXISTS test (
    message text NOT NULL
);

create user 'user8'@'%' identified by 'pass8';
GRANT ALL PRIVILEGES ON *.* TO 'user8'@'%' WITH GRANT OPTION;


CREATE DATABASE IF NOT EXISTS hellomemsql5;
USE hellomemsql5;
CREATE TABLE IF NOT EXISTS test (
    message text NOT NULL
);
create user 'user5'@'localhost' identified by 'pass5';
GRANT ALL ON hellomemsql5.*  TO 'user5'@'localhost';


CREATE DATABASE IF NOT EXISTS hellomemsql1;
USE hellomemsql1;
CREATE TABLE IF NOT EXISTS test (
    message text NOT NULL
);
create user 'user1'@'localhost' identified by 'pass1';
GRANT ALL ON hellomemsql1.*  TO 'user1'@'localhost';






CREATE DATABASE IF NOT EXISTS hellomemsql2;
USE hellomemsql2;
CREATE TABLE IF NOT EXISTS test (
    message text NOT NULL
);
create user 'user2'@'localhost' identified by 'pass2';
GRANT ALL PRIVILEGES ON hellomemsql2.*  TO 'user2'@'localhost';


CREATE DATABASE IF NOT EXISTS hellomemsql3;
USE hellomemsql3;
CREATE TABLE IF NOT EXISTS test (
    message text NOT NULL
);
create user 'user3'@'localhost' identified by 'pass3';
GRANT ALL PRIVILEGES ON hellomemsql3.*  TO 'user3'@'localhost';


CREATE DATABASE IF NOT EXISTS hellomemsql4;
USE hellomemsql4;
CREATE TABLE IF NOT EXISTS test (
    message text NOT NULL
);
create user 'user4'@'localhost' identified by 'pass4';
GRANT ALL PRIVILEGES ON *.*  TO 'user4'@'localhost';

FLUSH PRIVILEGES;

