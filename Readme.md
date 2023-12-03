# milvus-sql-proxy
Milvus SQL Proxy is a proxy service that translates SQL queries into Milvus[https://milvus.io] grpc requests. Make integration with milvus easier.

It can function as a client side sidecar proxy, or a server side proxy, so that you can use various sql driver to connect to milvus.

It's still in early alpha stage.


## Get Started

1. install milvus-lite: `python3 -m pip install milvus`
2. run milvus-server: `milvus-server`
3. run milvus-sql-proxy: `go run cmd/milvus-sql.go`
4. run mysql client: `mysql -u root -h 127.0.0.1 -P 3306`

## Supported Commands
- [x] show databases
- [x] create database
- [x] use database
- [x] drop database
- [x] show tables
- [x] create table
  - [x] auto increment primary key
  - [ ] with index
- [x] drop table
- [x] insert
- [ ] create index
- [ ] load
- [ ] release
- [x] select
  - [x] scalar query
  - [] vector search
- [ ] delete

## What we want to implement

To use MySQL protocol to connect to & operate [Milvus](https://milvus.io/)

```sql
show databases;
/* output:
+-----------+
| DATABASES |
+-----------+
| default   |
+-----------+
1 rows in set (0.01 sec)
*/

-- create database
create database mydb;
/* output:
Query OK, 1 row affected (0.04 sec)
*/

use mydb;
/* output:
Database changed
*/

-- create collection
create table test (
    id bigint AUTO_INCREMENT PRIMARY KEY, 
    name varchar(255),
    vec vector(32)); -- NOTE: zilliz cloud supports >=32 dimension

-- create vector index
create HNSW("L2") index vec_idx on test (vec);

-- insert vector
insert into test (name, vec) values 
    ("jack", json_vector("[1.0,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32]")),
    ("tom",  json_vector("[2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33]")),
    ("lucy", json_vector("[3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33,34]")),
    ("lily", json_vector("[4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33,34,35]")),
    ("nova", json_vector("[5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33,34,35,36]")),
    ("peter", json_vector("[6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,34,35,36,37,38]")),
    ("john", json_vector("[7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,32,33,34,35,36,37,38,39]")),
    ("jason", json_vector("[8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,29,30,31,32,33,34,35,36,37,38,39,40]"));
/* output:
Query OK, 8 row affected (0.10 sec)
*/

-- ANN search
select id from test where vec like json_vector("[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32]") limit 3;
-- simple query
select * from test where id=1;

-- delete data
delete from test where id = 1;

-- delete collection
truncate table test;

-- drop database
drop database mydb;
```

# Thanks
- http://github.com/xwb1989/sqlparser for the brilliant sql parser.
- http://github.com/flike/kingshard for their sql proxy server framework.
