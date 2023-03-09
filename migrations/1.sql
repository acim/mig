CREATE TABLE IF NOT EXISTS users (
	user_id serial PRIMARY KEY,
	username VARCHAR (50) UNIQUE NOT NULL
);

INSERT INTO users (username) VALUES ('zika');

DROP TABLE users;
