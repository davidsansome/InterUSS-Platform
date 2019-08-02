package dss

import (
	"bufio"
	"bytes"
	"errors"
	"strconv"
	"strings"

	"github.com/golang/geo/s2"
)

var (
	errOddNumberOfCoordinatesInAreaString = errors.New("odd number of coordinates in area string")
	errNotEnoughPointsInPolygon           = errors.New("not enough points in polygon")
)

func splitAtComma(data []byte, atEOF bool) (int, []byte, error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}

	if i := bytes.IndexByte(data, ','); i >= 0 {
		return i + 1, data[:i], nil
	}

	if atEOF {
		return len(data), data, nil
	}

	return 0, nil, nil
}

// parseArea parses "area" in the format 'lat0,lon0,lat1,lon1,...'
// and returns the resulting Loop.
func parseArea(area string) (*s2.Loop, error) {
	var (
		lat, lng = float64(0), float64(0)
		points   = []s2.Point{}
		counter  = 0
		scanner  = bufio.NewScanner(strings.NewReader(area))
	)
	scanner.Split(splitAtComma)

	for scanner.Scan() {
		switch counter % 2 {
		case 0:
			f, err := strconv.ParseFloat(scanner.Text(), 64)
			if err != nil {
				return nil, err
			}
			lat = f
		case 1:
			f, err := strconv.ParseFloat(scanner.Text(), 64)
			if err != nil {
				return nil, err
			}
			lng = f

			points = append(points, s2.PointFromLatLng(s2.LatLngFromDegrees(lat, lng)))
		}

		counter++
	}

	switch {
	case counter%2 != 0:
		return nil, errOddNumberOfCoordinatesInAreaString
	case len(points) < 3:
		return nil, errNotEnoughPointsInPolygon
	}

	return s2.LoopFromPoints(points), nil
}
