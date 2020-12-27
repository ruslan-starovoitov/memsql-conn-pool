CREATE DATABASE IF NOT EXISTS hellomemsql2;
USE hellomemsql2;
CREATE TABLE IF NOT EXISTS test (
    message text NOT NULL
);
create user 'user2'@'localhost' identified by 'pass2';


#
# CREATE DATABASE hellomemsql1;
# USE hellomemsql1;
# CREATE TABLE IF NOT EXISTS test (
#     message text NOT NULL
# );
# create user 'user1'@'localhost' identified by 'pass1';
#
#
#
# USE hellomemsql;
# CREATE TABLE IF NOT EXISTS test (
#     message text NOT NULL
# );
#
#
#

# select table_name from information_schema.tables
# show users