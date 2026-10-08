CREATE TABLE golang_kp.actors (
    id SERIAL PRIMARY KEY,
    version BIGINT NOT NULL DEFAULT 1,
    first_name VARCHAR(100) NOT NULL,
    last_name VARCHAR(100) NOT NULL,
    middle_name VARCHAR(100),
    description TEXT,
    birth_date DATE NOT NULL
);