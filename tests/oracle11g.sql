-- Oracle 11g-compatible initialization script for targeted read-path tests

CREATE TABLE users (
  id NUMBER PRIMARY KEY,
  full_name VARCHAR2(100) NOT NULL,
  email VARCHAR2(100) UNIQUE NOT NULL,
  stripe_id VARCHAR2(255),
  latest_audit_log_id NUMBER,
  disabled NUMBER(1) DEFAULT 0,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP NOT NULL
);

CREATE TABLE products (
  id NUMBER PRIMARY KEY,
  name VARCHAR2(100) NOT NULL,
  description VARCHAR2(255),
  price NUMBER(10, 2) NOT NULL,
  count_likes NUMBER,
  owner_id NUMBER NOT NULL REFERENCES users(id),
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP NOT NULL
);

INSERT INTO users (id, full_name, email, stripe_id, latest_audit_log_id, disabled, created_at)
SELECT LEVEL,
  'User ' || LEVEL,
  'user' || LEVEL || '@test.com',
  'payment_id_' || (LEVEL + 1000),
  CASE WHEN LEVEL <= 3 THEN LEVEL ELSE NULL END,
  CASE WHEN LEVEL = 50 THEN 1 ELSE 0 END,
  TO_TIMESTAMP('2021-01-09 16:37:01', 'YYYY-MM-DD HH24:MI:SS')
FROM DUAL CONNECT BY LEVEL <= 100;

INSERT INTO products (id, name, description, price, count_likes, owner_id, created_at)
SELECT LEVEL,
  'Product ' || LEVEL,
  'Description for product ' || LEVEL,
  LEVEL + 10.5,
  NULL,
  LEVEL,
  TO_TIMESTAMP('2021-01-09 16:37:01', 'YYYY-MM-DD HH24:MI:SS')
FROM DUAL CONNECT BY LEVEL <= 100;
