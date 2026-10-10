CREATE TABLE golang_kp.movie_actors(
    movie_id INT NOT NULL REFERENCES golang_kp.movies(id) ON DELETE CASCADE,
    actor_id INT NOT NULL REFERENCES golang_kp.actors(id) ON DELETE CASCADE,
    PRIMARY KEY (movie_id, actor_id)
);

CREATE INDEX movie_actors_actor_id_dnx ON golang_kp.movie_actors (actor_id);