package context

// MobHandler defines the interface for handling mob data in the game world.
type MobHandler interface {
	GetId() int
	GetSpecies() int
	GetHealth() (int, int)
	SubHealth(hp int) bool
	GetExperience() uint64
	GetPosition() Position
}
