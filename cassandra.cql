create keyspace cluegetter WITH REPLICATION = { 'class' : 'SimpleStrategy', 'replication_factor' : 1 };
use cluegetter;

CREATE TABLE message (
  message text PRIMARY KEY,
  instance text,
  date timestamp,
  body text
);
