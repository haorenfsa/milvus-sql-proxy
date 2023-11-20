# milvus-sql-proxy
Milvus SQL Proxy is a proxy service that translates SQL queries into Milvus grpc requests. Make integration with milvus easier.

It's still in early alpha stage.

## What we want to implement

To use MySQL protocol to connect to & operate [Milvus](https://milvus.io/)

```sql
-- create database
create database mydb;

use mydb;

-- create collection
create table test (
    id int64, 
    vec vector);

-- insert vector
insert into test values (1, [1.0, 2.0, 3.0, 4.0]);

-- create vector index
create HNSW index vec_idx on test (vec);

-- ANN search
select id from test where vec like ANN(vec) limit 10;

-- delete data
delete from test where id = 1;

-- delete collection
truncate table test;

-- drop database
drop database mydb;
```
