ALTER TABLE golang_kp.movie_actors
    ADD CONSTRAINT movie_actors_movie_id_fkey
        FOREIGN KEY (actor_id) REFERENCES golang_kp.actors (id) ON DELETE CASCADE;