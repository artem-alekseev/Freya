package game

import (
	"sync"

	"github.com/ubis/Freya/cmd/gameserver/context"
	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/models/drop"
	"github.com/ubis/Freya/share/network"
)

// WorldManager manages world maps and their data
type WorldManager struct {
	SpawnMobs bool
	Worlds    []*World
	Mobs      []*Mob

	dropMutex sync.RWMutex
	dropList  map[uint32][]drop.Entry
}

// SetDropList replaces the in-memory copy of the database-backed drop list.
func (wm *WorldManager) SetDropList(entries []drop.Entry) {
	bySpecies := make(map[uint32][]drop.Entry)
	for _, entry := range entries {
		if entry.MobSpecies == 0 || entry.ItemKind == 0 || entry.Amount == 0 ||
			entry.ChanceBPS == 0 || entry.ChanceBPS > 10000 {
			continue
		}

		bySpecies[entry.MobSpecies] = append(bySpecies[entry.MobSpecies], entry)
	}

	wm.dropMutex.Lock()
	wm.dropList = bySpecies
	wm.dropMutex.Unlock()
}

// GetDropList returns a copy so callers can roll drops without holding the
// manager lock or mutating the shared rule set.
func (wm *WorldManager) GetDropList(species uint32) []drop.Entry {
	wm.dropMutex.RLock()
	entries := wm.dropList[species]
	result := append([]drop.Entry(nil), entries...)
	wm.dropMutex.RUnlock()
	return result
}

// Initialize loads worlds, their data and initializes worlds.
func (wm *WorldManager) Initialize() {
	log.Info("Initializing World Manager...")

	// load world data
	if err := load("world.yml", &wm.Worlds); err != nil {
		log.Error("Failed to load world data:", err.Error())
		return
	}

	if wm.SpawnMobs {
		// load mob templates only when spawning is enabled
		if err := load("mobs.yml", &wm.Mobs); err != nil {
			log.Error("Failed to load world mob data:", err.Error())
			return
		}
	} else {
		log.Info("Mob spawning is disabled by configuration")
	}

	log.Infof("Loaded %d world maps\n", len(wm.Worlds))
	if wm.SpawnMobs {
		log.Infof("Loaded %d mobs\n", len(wm.Mobs))
	}

	// initialize each world
	for _, v := range wm.Worlds {
		v.Initialize(wm)
	}
}

// FindWorld finds a world by its ID.
func (wm *WorldManager) FindWorld(id byte) context.WorldHandler {
	for _, v := range wm.Worlds {
		if v.Id == id {
			return v
		}
	}

	return nil
}

// BroadcastAllPacket sends a packet to every player on every loaded map.
func (wm *WorldManager) BroadcastAllPacket(pkt *network.Writer) {
	for _, world := range wm.Worlds {
		world.BroadcastAllPacket(pkt)
	}
}

// GetMob returns the mob that matches a given species ID.
func (wm *WorldManager) GetMob(species int) *Mob {
	for _, v := range wm.Mobs {
		if v.Species != species {
			continue
		}

		return v
	}

	return nil
}
