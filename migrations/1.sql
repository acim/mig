BEGIN;

CREATE TABLE IF NOT EXISTS accounts (
	user_id serial PRIMARY KEY,
	username VARCHAR (50) UNIQUE NOT NULL
);

INSERT INTO accounts (username) VALUES ('zika');

COMMIT;
