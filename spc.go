package main

import (
	"encoding/xml"
	"flag"
	"fmt"
	// "github.com/kr/pretty"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	timefmt = "3:04pm 1/2"
)

var loc *time.Location

type kmlDoc struct {
	Name        string      `xml:"Document>name"`
	Folders     []kmlFolder `xml:"Document>Folder"`
	Description string      `xml:"Document>description",cdata`
}

func (k kmlDoc) Updated() time.Time {
	updated := strings.Split(k.Description, "\n")[2]
	t, err := time.Parse("Issue Time 0304 PM MST Mon Jan 02 2006<br />", updated)
	if err != nil {
		// Fallback format
		t, err = time.Parse("Issue Time 20060102 200601021504Z<br />", updated)
	}
	if err != nil {
		fmt.Println(err)
		return time.Time{}
	}
	return t
}

type kmlFolder struct {
	Name       string         `xml:"name"`
	Placemarks []kmlPlacemark `xml:"Placemark"`
}

func (k kmlFolder) Tag() string {
	words := strings.Split(k.Name, "_")
	result := ""
	switch words[len(words)-1] {
	case "cat":
		result = "Categorical Risk for severe weather"
	case "wind":
		result = "Risk of >57 mph gusts within 25 miles"
	case "hail":
		result = "Risk of 1\" hail within 25 miles"
	case "torn":
		result = "Risk of tornado within 25 miles"
	case "sigwind":
		result = "Significant wind (>75 mph gusts) within 25 miles"
	case "sighail":
		result = "Significant hail (>2\") within 25 miles"
	case "sigtorn":
		result = "Significant tornado (EF2 or greater) within 25 miles"
	default:
		// Assume it's a tstm file:
		result = "Risk of T'Strm within 12 miles"
	}
	return result
}

type kmlPlacemark struct {
	BeginStr string          `xml:"TimeSpan>begin"`
	EndStr   string          `xml:"TimeSpan>end"`
	Name     string          `xml:"name"`
	Outer    kmlLinearRing   `xml:"Polygon>outerBoundaryIs>LinearRing"`
	Inner    []kmlLinearRing `xml:"Polygon>innerBoundaryIs>LinearRing"`
}

const kmlTimeFmt = time.RFC3339

func (k kmlPlacemark) Begin() (time.Time, error) {
	t, err := time.Parse(kmlTimeFmt, k.BeginStr)
	return t.In(loc), err
}

func (k kmlPlacemark) End() (time.Time, error) {
	t, err := time.Parse(kmlTimeFmt, k.EndStr)
	return t.In(loc), err
}

type Coord struct {
	Lat float64
	Lng float64
}

type kmlLinearRing struct {
	CoordData string `xml:"coordinates"`
}

func (s kmlLinearRing) Coords() ([]Coord, error) {
	results := []Coord{}
	points := strings.Split(s.CoordData, " ")
	for _, point := range points {
		spot := strings.Split(point, ",")
		coord := Coord{}
		if lat, err := strconv.ParseFloat(spot[1], 64); err != nil {
			return results, err
		} else {
			coord.Lat = lat
		}
		if lng, err := strconv.ParseFloat(spot[0], 64); err != nil {
			return results, err
		} else {
			coord.Lng = lng
		}
		results = append(results, coord)
	}
	return results, nil
}

func isLeft(p0, p1, p2 Coord) float64 {
	return (p1.Lng-p0.Lng)*(p2.Lat-p0.Lat) - (p2.Lng-p0.Lng)*(p1.Lat-p0.Lat)
}

// Mostly stolen from http://geomalgorithms.com/a03-_inclusion.html
// returns 0 when p is outside v
func windingNumberTest(p Coord, v []Coord) float64 {
	wn := 0.0
	for i := 0; i < len(v)-1; i++ {
		if v[i].Lat <= p.Lat {
			if v[i+1].Lat > p.Lat {
				if isLeft(v[i], v[i+1], p) > 0 {
					wn++
				}
			}
		} else {
			if v[i+1].Lat <= p.Lat {
				if isLeft(v[i], v[i+1], p) < 0 {
					wn--
				}
			}
		}
	}
	return wn
}

func main() {
	loc, _ = time.LoadLocation("America/Chicago")
	testPoint := Coord{}
	var kmlFileName = flag.String("kml", "", "name of kml file to process")
	var latparam = flag.Float64("lat", 0, "Latitude for weather status")
	var lngparam = flag.Float64("lng", 0, "Longitude for weather status")
	flag.Parse()
	testPoint.Lat = *latparam
	testPoint.Lng = *lngparam

	if testPoint.Lat == 0 {
		fmt.Println("You forgot to specify -lat FLOAT64")
	}
	if testPoint.Lng == 0 {
		fmt.Println("You forgot to specify -lng FLOAT64")
	}

	if len(*kmlFileName) <= 0 {
		fmt.Println("You forgot to specify -kml FILENAME")
		os.Exit(1)
	}

	kmlFile, err := os.Open(*kmlFileName)
	if err != nil {
		fmt.Println("Error opening KML File: ", err)
		os.Exit(1)
	}
	defer kmlFile.Close()

	var doc kmlDoc
	b, _ := ioutil.ReadAll(kmlFile)
	xml.Unmarshal(b, &doc)

	fmt.Printf("Issued: %s\n", doc.Updated().Format(timefmt))

	beginT := time.Time{}
	endT := time.Time{}
	noSignWeather := true

FolderLoop:
	for _, folder := range doc.Folders {
		for _, placemark := range folder.Placemarks {
			newBT, _ := placemark.Begin()
			newET, _ := placemark.End()
			if !(newBT.Equal(beginT) && (newET.Equal(endT))) {
				fmt.Printf("Valid %s — %s\n\n",
					newBT.Format(timefmt),
					newET.Format(timefmt))
				beginT = newBT
				endT = newET
			}

			shape, _ := placemark.Outer.Coords()
			if windingNumberTest(testPoint, shape) != 0 {
				for _, inner := range placemark.Inner {
					shape, _ := inner.Coords()
					if windingNumberTest(testPoint, shape) != 0 {
						// toss out - point inside inner poly
						continue FolderLoop
					}
				}
				// here testPoint is inside outer polly but outside inner ones
				fmt.Printf("%s: %s\n", folder.Tag(), placemark.Name)
				noSignWeather = false
			}
		}
	}
	if noSignWeather {
		fmt.Printf("No significant threat\n")
	}
}
