package clientdata

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
)

// WarpLocation contains a destination coordinate for one nation variant.
type WarpLocation struct {
	X byte
	Y byte
}

// WarpPoint is one entry from cabal.dec's warp_point table.
// The ID is the zero-based record index used by WorldSvr's Warp.scp table.
type WarpPoint struct {
	ID    uint16
	World byte
	// Locations are ordered as NATION_NONE, NATION_CAPELLA, NATION_PROCYON.
	Locations [3]WarpLocation
	Code      byte
	Fee       uint32
	Level     uint16
}

// WarpRoute describes one destination from cabal.dec's warp_npc table.
// WarpIndex is the one-based target_id used by warp_list; WarpPoint IDs remain
// zero-based because they address the parsed cabal.dec records.
type WarpRoute struct {
	SourceWorld byte
	WarpIndex   uint16
	Order       uint16
	NPC         byte
	Level       uint16
}

type mapWarpIndices struct {
	DeadWarp   uint16
	ReturnWarp uint16
}

var warpPoints []WarpPoint
var warpRoutes []WarpRoute
var mapWarps = make(map[byte]mapWarpIndices)

// returnStoneItemIndex is ITEMINDEX4WARPSCRLL from WorldSvr. The item kind
// can contain flags, so callers must compare the masked item index.
const returnStoneItemIndex uint32 = 12

// IsReturnStone reports whether itemKind is the one-use return stone used by
// NPCSIDX_RETN. The timed return core is a different item and is not consumed.
func IsReturnStone(itemKind uint32) bool {
	return itemKind&itemDataKindMask == returnStoneItemIndex
}

// WarpPoints returns a copy of the client warp table.
func WarpPoints() []WarpPoint {
	return append([]WarpPoint(nil), warpPoints...)
}

func parseWarpRoutes(cabalData []byte) ([]WarpRoute, error) {
	xmlStart := bytes.Index(cabalData, []byte("<cabal>"))
	if xmlStart < 0 {
		return nil, fmt.Errorf("cabal.dec does not contain a cabal XML section")
	}

	decoder := xml.NewDecoder(bytes.NewReader(cabalData[xmlStart:]))
	routes := make([]WarpRoute, 0)
	var sourceWorld byte
	var npc byte
	inWarpNPC := false

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse cabal.dec warp routes: %w", err)
		}

		switch element := token.(type) {
		case xml.StartElement:
			switch element.Name.Local {
			case "warp_npc":
				inWarpNPC = true
			case "world":
				if !inWarpNPC {
					continue
				}
				value, err := uintAttribute(element, "id", 8)
				if err != nil {
					return nil, err
				}
				sourceWorld = byte(value)
			case "npc":
				if !inWarpNPC {
					continue
				}
				value, err := uintAttribute(element, "id", 8)
				if err != nil {
					return nil, err
				}
				npc = byte(value)
			case "warp_list":
				if !inWarpNPC || sourceWorld == 0 {
					continue
				}
				warpType, err := uintAttribute(element, "type", 8)
				if err != nil {
					return nil, err
				}
				if warpType != 0 {
					continue
				}
				order, err := uintAttribute(element, "order", 16)
				if err != nil {
					return nil, err
				}
				targetID, err := uintAttribute(element, "target_id", 16)
				if err != nil {
					return nil, err
				}
				level, err := uintAttribute(element, "level", 16)
				if err != nil {
					return nil, err
				}
				routes = append(routes, WarpRoute{
					SourceWorld: sourceWorld,
					WarpIndex:   uint16(targetID),
					Order:       uint16(order),
					NPC:         npc,
					Level:       uint16(level),
				})
			}
		case xml.EndElement:
			switch element.Name.Local {
			case "warp_npc":
				inWarpNPC = false
				sourceWorld = 0
				npc = 0
			case "world":
				if inWarpNPC {
					sourceWorld = 0
					npc = 0
				}
			case "npc":
				if inWarpNPC {
					npc = 0
				}
			}
		}
	}

	if len(routes) == 0 {
		return nil, fmt.Errorf("cabal.dec does not contain warp_npc routes")
	}

	return routes, nil
}

// FindNPCWarp resolves a normal NPC warp to the destination point. The
// client sends the NPC warp-set index in the union field, while cabal.dec
// provides the source-world/NPC/order to warp-index mapping.
func FindNPCWarp(sourceWorld, npc byte, setIndex uint32) (WarpPoint, bool) {
	var candidates []WarpRoute
	for _, route := range warpRoutes {
		if route.SourceWorld == sourceWorld && route.NPC == npc {
			candidates = append(candidates, route)
		}
	}
	if len(candidates) == 0 {
		return WarpPoint{}, false
	}

	selected := -1
	for i, route := range candidates {
		if uint32(route.Order) == setIndex {
			selected = i
			break
		}
	}
	if selected < 0 && setIndex < uint32(len(candidates)) {
		selected = int(setIndex)
	}
	if selected < 0 {
		return WarpPoint{}, false
	}

	return warpPointByOneBasedIndex(candidates[selected].WarpIndex)
}

// FindMapTransition resolves the special NPCSIDS_BPNT command used by the
// client when it crosses a map boundary. The packet does not contain the
// source NPC; the selected route is represented by the slot/order index.
func FindMapTransition(sourceWorld byte, routeIndex uint16) (WarpPoint, bool) {
	candidates := make([]WarpPoint, 0)
	for _, route := range warpRoutes {
		if route.SourceWorld != sourceWorld {
			continue
		}

		point, ok := warpPointByOneBasedIndex(route.WarpIndex)
		if !ok || point.World == sourceWorld {
			continue
		}
		candidates = append(candidates, point)
	}

	if len(candidates) == 0 {
		return WarpPoint{}, false
	}
	if routeIndex == 0xffff {
		routeIndex = 0
	}
	if int(routeIndex) >= len(candidates) {
		return WarpPoint{}, false
	}

	return candidates[routeIndex], true
}

// FindReturnPoint returns the nation-specific return location. The first two
// code=1 records for a world are the Capella and Procyon return points in the
// client table.
func FindReturnPoint(world, nation byte) (WarpPoint, bool) {
	if indices, ok := mapWarps[world]; ok {
		if point, found := warpPointByOneBasedIndex(indices.ReturnWarp); found {
			return point, true
		}
	}

	matches := make([]WarpPoint, 0, 2)
	for _, point := range warpPoints {
		if point.World == world && point.Code == 1 {
			matches = append(matches, point)
		}
	}
	if len(matches) == 0 {
		return WarpPoint{}, false
	}

	if nation == 2 && len(matches) > 1 {
		return matches[1], true
	}
	return matches[0], true
}

// FindStartingPoint returns the map's dead/start warp from cabal.dec. It is
// separate from FindReturnPoint because the two indices can differ on dungeon
// and event maps.
func FindStartingPoint(world, nation byte) (WarpPoint, bool) {
	if indices, ok := mapWarps[world]; ok {
		if point, found := warpPointByOneBasedIndex(indices.DeadWarp); found {
			return point, true
		}
	}

	return FindReturnPoint(world, nation)
}

func warpPointByOneBasedIndex(index uint16) (WarpPoint, bool) {
	if index == 0 {
		return WarpPoint{}, false
	}
	zeroBased := index - 1
	if zeroBased >= uint16(len(warpPoints)) {
		return WarpPoint{}, false
	}

	return warpPoints[zeroBased], true
}

func parseMapWarpIndices(cabalData []byte) (map[byte]mapWarpIndices, error) {
	xmlStart := bytes.Index(cabalData, []byte("<cabal>"))
	if xmlStart < 0 {
		return nil, fmt.Errorf("cabal.dec does not contain a cabal XML section")
	}

	decoder := xml.NewDecoder(bytes.NewReader(cabalData[xmlStart:]))
	loaded := make(map[byte]mapWarpIndices)
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse cabal.dec map warp indices: %w", err)
		}

		element, ok := token.(xml.StartElement)
		if !ok || element.Name.Local != "map_index" {
			continue
		}

		world, err := uintAttribute(element, "world_id", 8)
		if err != nil {
			return nil, err
		}
		deadWarp, err := uintAttribute(element, "dead_warp", 16)
		if err != nil {
			return nil, err
		}
		returnWarp, err := uintAttribute(element, "return_warp", 16)
		if err != nil {
			return nil, err
		}

		loaded[byte(world)] = mapWarpIndices{
			DeadWarp:   uint16(deadWarp),
			ReturnWarp: uint16(returnWarp),
		}
	}

	return loaded, nil
}

func parseWarpPoints(cabalData []byte) ([]WarpPoint, error) {
	xmlStart := bytes.Index(cabalData, []byte("<cabal>"))
	if xmlStart < 0 {
		return nil, fmt.Errorf("cabal.dec does not contain a cabal XML section")
	}

	decoder := xml.NewDecoder(bytes.NewReader(cabalData[xmlStart:]))
	loaded := make([]WarpPoint, 0)
	inWarpPointTable := false

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse cabal.dec warp points: %w", err)
		}

		switch element := token.(type) {
		case xml.StartElement:
			switch element.Name.Local {
			case "warp_point":
				inWarpPointTable = true
			case "warp_index":
				if !inWarpPointTable {
					continue
				}

				point, err := parseWarpPoint(element, len(loaded))
				if err != nil {
					return nil, err
				}
				loaded = append(loaded, point)
			}
		case xml.EndElement:
			if element.Name.Local == "warp_point" {
				inWarpPointTable = false
			}
		}
	}

	if len(loaded) == 0 {
		return nil, fmt.Errorf("cabal.dec does not contain warp points")
	}

	return loaded, nil
}

func parseWarpPoint(element xml.StartElement, index int) (WarpPoint, error) {
	if index > int(^uint16(0)) {
		return WarpPoint{}, fmt.Errorf("warp point index %d exceeds uint16", index)
	}

	readByte := func(name string) (byte, error) {
		value, err := uintAttribute(element, name, 8)
		if err != nil {
			return 0, err
		}
		return byte(value), nil
	}
	readUint16 := func(name string) (uint16, error) {
		value, err := uintAttribute(element, name, 16)
		if err != nil {
			return 0, err
		}
		return uint16(value), nil
	}
	readUint32 := func(name string) (uint32, error) {
		value, err := uintAttribute(element, name, 32)
		if err != nil {
			return 0, err
		}
		return uint32(value), nil
	}

	world, err := readByte("WorldIdx")
	if err != nil {
		return WarpPoint{}, err
	}
	code, err := readByte("w_code")
	if err != nil {
		return WarpPoint{}, err
	}
	fee, err := readUint32("Fee")
	if err != nil {
		return WarpPoint{}, err
	}
	level, err := readUint16("level")
	if err != nil {
		return WarpPoint{}, err
	}

	x, err := readByte("x")
	if err != nil {
		return WarpPoint{}, err
	}
	y, err := readByte("y")
	if err != nil {
		return WarpPoint{}, err
	}
	nation1X, err := readByte("nation1x")
	if err != nil {
		return WarpPoint{}, err
	}
	nation1Y, err := readByte("nation1y")
	if err != nil {
		return WarpPoint{}, err
	}
	nation2X, err := readByte("nation2x")
	if err != nil {
		return WarpPoint{}, err
	}
	nation2Y, err := readByte("nation2y")
	if err != nil {
		return WarpPoint{}, err
	}

	return WarpPoint{
		ID:    uint16(index),
		World: world,
		Locations: [3]WarpLocation{
			{X: x, Y: y},
			{X: nation1X, Y: nation1Y},
			{X: nation2X, Y: nation2Y},
		},
		Code:  code,
		Fee:   fee,
		Level: level,
	}, nil
}
