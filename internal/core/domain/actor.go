package domain

import "time"

type Actor struct {
	ID          int
	Version     int
	FirstName   string
	LastName    string
	MiddleName  *string
	Description *string
	BirthDate   time.Time
}

func NewActor(
	id int,
	version int,
	firstName string,
	lastName string,
	middleName *string,
	description *string,
	birthDate time.Time,
) Actor {
	return Actor{
		ID:          id,
		Version:     version,
		FirstName:   firstName,
		LastName:    lastName,
		MiddleName:  middleName,
		Description: description,
		BirthDate:   birthDate,
	}
}

func NewActorUninitialized(
	firstName string,
	lastName string,
	middleName *string,
	description *string,
	birthDate time.Time,
) Actor {
	return NewActor(
		UninitializedID,
		UninitializedVersion,
		firstName,
		lastName,
		middleName,
		description,
		birthDate,
	)
}

func (a *Actor) ApplyPatch(patch ActorPatch) {
	if patch.FirstName != nil {
		a.FirstName = *patch.FirstName
	}

	if patch.LastName != nil {
		a.LastName = *patch.LastName
	}

	if patch.MiddleName.Set {
		if patch.MiddleName.Null {
			a.MiddleName = nil
		} else {
			a.MiddleName = &patch.MiddleName.Value
		}
	}

	if patch.Description.Set {
		if patch.Description.Null {
			a.Description = nil
		} else {
			a.Description = &patch.Description.Value
		}
	}

	if patch.BirthDate != nil {
		a.BirthDate = *patch.BirthDate
	}
}

type ActorPatch struct {
	FirstName   *string
	LastName    *string
	MiddleName  Optional[string]
	Description Optional[string]
	BirthDate   *time.Time
}

func NewActorPatch(
	firstName *string,
	lastName *string,
	middleName Optional[string],
	description Optional[string],
	birthDate *time.Time,
) ActorPatch {
	return ActorPatch{
		FirstName:   firstName,
		LastName:    lastName,
		MiddleName:  middleName,
		Description: description,
		BirthDate:   birthDate,
	}
}
