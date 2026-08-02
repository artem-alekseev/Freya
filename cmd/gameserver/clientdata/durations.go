package clientdata

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"time"
)

type itemDuration struct {
	Days    int
	Hours   int
	Minutes int
}

var itemDurations = make(map[byte]itemDuration)

func parseItemDurations(data []byte) (map[byte]itemDuration, error) {
	loaded := make(map[byte]itemDuration)
	decoder := xml.NewDecoder(bytes.NewReader(data))

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse caz.dec XML: %w", err)
		}

		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "duration" {
			continue
		}

		id, err := uintAttribute(start, "id", 8)
		if err != nil {
			return nil, err
		}
		days, err := intAttribute(start, "day", 16)
		if err != nil {
			return nil, err
		}
		hours, err := intAttribute(start, "hour", 16)
		if err != nil {
			return nil, err
		}
		minutes, err := intAttribute(start, "min", 16)
		if err != nil {
			return nil, err
		}
		if days < 0 || hours < 0 || minutes < 0 {
			return nil, fmt.Errorf("invalid item duration %d", id)
		}

		loaded[byte(id)] = itemDuration{
			Days:    int(days),
			Hours:   int(hours),
			Minutes: int(minutes),
		}
	}

	if len(loaded) == 0 {
		return nil, fmt.Errorf("caz.dec does not contain item durations")
	}
	return loaded, nil
}

// CashItemExpiration converts the client's duration index to the packed
// PERIOD value used by inventory items. Indices 0 and 31 mean unlimited.
func CashItemExpiration(durationID byte) (uint32, bool) {
	if durationID == 0 || durationID == 31 {
		return uint32(durationID) << 27, true
	}

	duration, ok := itemDurations[durationID]
	if !ok || duration.Days == 0 && duration.Hours == 0 && duration.Minutes == 0 {
		return 0, false
	}

	value := time.Now().AddDate(0, 0, duration.Days).
		Add(time.Duration(duration.Hours)*time.Hour + time.Duration(duration.Minutes)*time.Minute)
	year := value.Year() - 2000
	if year < 0 || year > 127 || value.Month() > 12 || value.Day() > 31 ||
		value.Hour() > 23 || value.Minute() > 63 {
		return 0, false
	}

	return uint32(year) |
		uint32(value.Month())<<7 |
		uint32(value.Day())<<11 |
		uint32(value.Hour())<<16 |
		uint32(value.Minute())<<21 |
		uint32(durationID)<<27, true
}
