package domain

import (
	"time"
)

type Movie struct {
	ID          int
	Version     int
	Title       string
	Description *string
	ReleaseAt   time.Time
	PosterKey   *string
}

func NewMovie(
	id int,
	version int,
	title string,
	description *string,
	releaseAt time.Time,
	PosterKey *string,
) Movie {
	return Movie{
		ID:          id,
		Version:     version,
		Title:       title,
		Description: description,
		ReleaseAt:   releaseAt,
		PosterKey:   PosterKey,
	}
}

func NewMovieUninitialized(title string, description *string, releaseAt time.Time) Movie {
	return NewMovie(
		UninitializedID,
		UninitializedVersion,
		title,
		description,
		releaseAt,
		UninitializedPosterKey,
	)
}

func (m *Movie) ApplyPatch(patch MoviePatch) {
	if patch.Title != nil {
		m.Title = *patch.Title
	}

	if patch.ReleaseAt != nil {
		m.ReleaseAt = *patch.ReleaseAt
	}

	if patch.Description.Set {
		if patch.Description.Null {
			m.Description = nil
		} else {
			m.Description = &patch.Description.Value
		}
	}
}

type MoviePatch struct {
	Title       *string
	Description Optional[string]
	ReleaseAt   *time.Time
}

func NewMoviePatch(
	title *string,
	description Optional[string],
	releaseAt *time.Time,
) MoviePatch {
	return MoviePatch{
		Title:       title,
		Description: description,
		ReleaseAt:   releaseAt,
	}
}
