# milvus-sql-proxy
Milvus SQL Proxy is a proxy service that translates SQL queries into Milvus grpc requests. Make integration with milvus easier.

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
- [ ] drop table
- [ ] insert
- [ ] create index
- [ ] load
- [ ] release
- [ ] select
- [ ] delete

## What we want to implement

To use MySQL protocol to connect to & operate [Milvus](https://milvus.io/)

```sql
-- create database
create database mydb;

use mydb;

-- create collection
create table test (
    id bigint AUTO_INCREMENT PRIMARY KEY, 
    name varchar(255),
    vec vector(32)); -- NOTE: zilliz cloud supports >=32 dimension

-- insert vector
insert into test values (1, "jack", '[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32]');

-- create vector index
create HNSW("L2") index vec_idx on test (vec);

-- ANN search
select id from test where vec like '[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32]' limit 10;

-- delete data
delete from test where id = 1;

-- delete collection
truncate table test;

-- drop database
drop database mydb;
```
