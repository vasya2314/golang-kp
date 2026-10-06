package domain

import "time"

type Movie struct {
	ID        int
	Version   int
	Title     string
	ReleaseAt time.Time
}

func NewMovie(
	id int,
	version int,
	title string,
	releaseAt time.Time,
) Movie {
	return Movie{
		ID:        id,
		Version:   version,
		Title:     title,
		ReleaseAt: releaseAt,
	}
}

func NewMovieUninitialized(title string, releaseAt time.Time) Movie {
	return NewMovie(
		UninitializedID,
		UninitializedVersion,
		title,
		releaseAt,
	)
}
