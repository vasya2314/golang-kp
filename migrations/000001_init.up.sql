CREATE SCHEMA IF NOT EXISTS golang_kp;

CREATE TABLE golang_kp.movies (
    id SERIAL PRIMARY KEY,
    version BIGINT NOT NULL DEFAULT 1,
    title VARCHAR(100) NOT NULL,
    release_at DATE NOT NULL
);