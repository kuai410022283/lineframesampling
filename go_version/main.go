package main

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/fogleman/gg"
	"github.com/sqweek/dialog"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

const (
	version        = "1.1.0"
	requiredColumn = "ez_sys_geom"
)

type Point struct {
	Lon float64
	Lat float64
}

type SamplingResult struct {
	LineID      int
	Distance    float64
	Center      Point
	ShapePoints []Point
}

func getDistanceMeters(p1, p2 Point) float64 {
	const radius = 6371000
	phi1 := p1.Lat * math.Pi / 180
	phi2 := p2.Lat * math.Pi / 180
	dphi := (p2.Lat - p1.Lat) * math.Pi / 180
	dlambda := (p2.Lon - p1.Lon) * math.Pi / 180

	a := math.Sin(dphi/2)*math.Sin(dphi/2) +
		math.Cos(phi1)*math.Cos(phi2)*math.Sin(dlambda/2)*math.Sin(dlambda/2)
	return 2 * radius * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func getHeading(p1, p2 Point) float64 {
	avgLat := (p1.Lat + p2.Lat) / 2 * math.Pi / 180
	dx := (p2.Lon - p1.Lon) * math.Cos(avgLat)
	dy := p2.Lat - p1.Lat
	return math.Atan2(dy, dx)
}

func movePoint(point Point, dxMeters, dyMeters float64) Point {
	const radius = 6371000
	newLat := point.Lat + (dyMeters/radius)*(180/math.Pi)
	newLon := point.Lon + (dxMeters/(radius*math.Cos(point.Lat*math.Pi/180)))*(180/math.Pi)
	return Point{Lon: newLon, Lat: newLat}
}

func pointsToWKTPolygon(points []Point) string {
	if len(points) == 0 {
		return ""
	}

	coords := make([]string, 0, len(points)+1)
	for _, point := range points {
		coords = append(coords, fmt.Sprintf("%.8f %.8f", point.Lon, point.Lat))
	}
	if points[0] != points[len(points)-1] {
		coords = append(coords, fmt.Sprintf("%.8f %.8f", points[0].Lon, points[0].Lat))
	}
	return fmt.Sprintf("POLYGON ((%s))", strings.Join(coords, ", "))
}

func parseWKTLineString(wkt string) ([]Point, error) {
	re := regexp.MustCompile(`\((.*)\)`)
	match := re.FindStringSubmatch(wkt)
	if len(match) < 2 {
		return nil, errors.New("invalid WKT")
	}

	pairs := strings.Split(match[1], ",")
	points := make([]Point, 0, len(pairs))
	for _, pair := range pairs {
		parts := strings.Fields(strings.TrimSpace(pair))
		if len(parts) < 2 {
			return nil, errors.New("invalid coordinate pair")
		}

		lon, err := strconv.ParseFloat(parts[0], 64)
		if err != nil {
			return nil, fmt.Errorf("invalid longitude: %w", err)
		}
		lat, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			return nil, fmt.Errorf("invalid latitude: %w", err)
		}
		points = append(points, Point{Lon: lon, Lat: lat})
	}

	if len(points) < 2 {
		return nil, errors.New("not enough points")
	}
	return points, nil
}

func decodeCSVBytes(data []byte) (io.Reader, string, error) {
	if len(data) >= 3 && bytes.Equal(data[:3], []byte{0xEF, 0xBB, 0xBF}) {
		return bytes.NewReader(data[3:]), "utf-8-sig", nil
	}
	if utf8.Valid(data) {
		return bytes.NewReader(data), "utf-8", nil
	}

	decoded, err := io.ReadAll(transform.NewReader(bytes.NewReader(data), simplifiedchinese.GB18030.NewDecoder()))
	if err != nil {
		return nil, "", err
	}
	return bytes.NewReader(decoded), "gb18030", nil
}

func loadCSV(filePath string) ([][]string, string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, "", err
	}

	readerSource, encodingName, err := decodeCSVBytes(data)
	if err != nil {
		return nil, "", err
	}

	reader := csv.NewReader(readerSource)
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return nil, "", err
	}
	return records, encodingName, nil
}

func buildShapePoints(center Point, angle, size float64, shape string) []Point {
	points := make([]Point, 0)
	if shape == "circle" {
		const segments = 32
		for index := 0; index < segments; index++ {
			theta := (2 * math.Pi * float64(index)) / float64(segments)
			dx := size * math.Cos(theta)
			dy := size * math.Sin(theta)
			points = append(points, movePoint(center, dx, dy))
		}
		return points
	}

	half := size / 2
	cosA := math.Cos(angle)
	sinA := math.Sin(angle)
	offsets := [][2]float64{{-half, -half}, {half, -half}, {half, half}, {-half, half}}
	for _, offset := range offsets {
		rotX := offset[0]*cosA - offset[1]*sinA
		rotY := offset[0]*sinA + offset[1]*cosA
		points = append(points, movePoint(center, rotX, rotY))
	}
	return points
}

func processData(records [][]string, interval, size float64, shape string) ([][]Point, []SamplingResult, int) {
	if len(records) < 2 {
		return nil, nil, 0
	}

	header := records[0]
	geomIdx := -1
	for index, column := range header {
		if column == requiredColumn {
			geomIdx = index
			break
		}
	}
	if geomIdx == -1 {
		fmt.Printf("Error: required column '%s' not found. Columns: %v\n", requiredColumn, header)
		return nil, nil, 0
	}

	allPolylines := make([][]Point, 0)
	allResults := make([]SamplingResult, 0)
	skippedRows := 0

	for lineID, row := range records[1:] {
		if geomIdx >= len(row) {
			skippedRows++
			fmt.Printf("Warning: skipped row %d because '%s' is missing.\n", lineID+1, requiredColumn)
			continue
		}

		polyline, err := parseWKTLineString(row[geomIdx])
		if err != nil {
			skippedRows++
			fmt.Printf("Warning: skipped row %d because '%s' is invalid: %v\n", lineID+1, requiredColumn, err)
			continue
		}

		allPolylines = append(allPolylines, polyline)
		accumulatedDist := 0.0
		targetDist := 0.0

		for index := 0; index < len(polyline)-1; index++ {
			p1 := polyline[index]
			p2 := polyline[index+1]
			segLen := getDistanceMeters(p1, p2)
			if segLen == 0 {
				continue
			}

			angle := getHeading(p1, p2)
			for accumulatedDist+segLen >= targetDist {
				ratio := (targetDist - accumulatedDist) / segLen
				center := Point{
					Lon: p1.Lon + (p2.Lon-p1.Lon)*ratio,
					Lat: p1.Lat + (p2.Lat-p1.Lat)*ratio,
				}

				allResults = append(allResults, SamplingResult{
					LineID:      lineID,
					Distance:    targetDist,
					Center:      center,
					ShapePoints: buildShapePoints(center, angle, size, shape),
				})
				targetDist += interval
			}

			accumulatedDist += segLen
		}
	}

	return allPolylines, allResults, skippedRows
}

func normalizeColor(color string) string {
	if color == "" {
		return "#333333"
	}
	if strings.HasPrefix(color, "#") && (len(color) == 7 || len(color) == 4) {
		return color
	}
	return "#333333"
}

func runVisualization(polylines [][]Point, results []SamplingResult, lineColor string) {
	validPolylines := make([][]Point, 0)
	for _, polyline := range polylines {
		if len(polyline) >= 2 {
			validPolylines = append(validPolylines, polyline)
		}
	}
	if len(validPolylines) == 0 {
		fmt.Println("Visualization skipped: no valid polylines available.")
		return
	}

	fmt.Println("Generating preview image...")
	const (
		width  = 2000
		height = 1600
	)

	context := gg.NewContext(width, height)
	context.SetRGB(0.07, 0.07, 0.07)
	context.Clear()

	minLon, maxLon := 180.0, -180.0
	minLat, maxLat := 90.0, -90.0
	for _, polyline := range validPolylines {
		for _, point := range polyline {
			if point.Lon < minLon {
				minLon = point.Lon
			}
			if point.Lon > maxLon {
				maxLon = point.Lon
			}
			if point.Lat < minLat {
				minLat = point.Lat
			}
			if point.Lat > maxLat {
				maxLat = point.Lat
			}
		}
	}

	toCanvas := func(point Point) (float64, float64) {
		x := ((point.Lon-minLon)/(maxLon-minLon+1e-9))*(width-200) + 100
		y := (height - 200) - ((point.Lat-minLat)/(maxLat-minLat+1e-9))*(height-200) + 100
		return x, y
	}

	context.SetHexColor(normalizeColor(lineColor))
	context.SetLineWidth(2)
	for _, polyline := range validPolylines {
		x, y := toCanvas(polyline[0])
		context.MoveTo(x, y)
		for _, point := range polyline[1:] {
			x, y = toCanvas(point)
			context.LineTo(x, y)
		}
		context.Stroke()
	}

	step := 1
	if len(results) > 1000 {
		step = len(results) / 1000
	}
	for index := 0; index < len(results); index += step {
		result := results[index]

		context.SetHexColor("#00adb5")
		context.SetLineWidth(1)
		if len(result.ShapePoints) > 0 {
			x, y := toCanvas(result.ShapePoints[0])
			context.MoveTo(x, y)
			for _, point := range result.ShapePoints[1:] {
				x, y = toCanvas(point)
				context.LineTo(x, y)
			}
			context.ClosePath()
			context.Stroke()
		}

		context.SetHexColor("#ff2e63")
		centerX, centerY := toCanvas(result.Center)
		context.DrawCircle(centerX, centerY, 3)
		context.Fill()
	}

	outputImage := "sampling_result_view.png"
	if err := context.SavePNG(outputImage); err != nil {
		fmt.Printf("Warning: failed to save preview image: %v\n", err)
		return
	}
	fmt.Printf("Preview saved to: %s\n", outputImage)
	_ = exec.Command("cmd", "/c", "start", outputImage).Run()
}

func printTemplate() {
	fmt.Println("CSV template")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("Required column: %s\n", requiredColumn)
	fmt.Println("Format example:")
	fmt.Println("name,ez_sys_geom,remark")
	fmt.Println(`"Demo line","LINESTRING(121.5000 38.9000, 121.6000 39.0000)","optional"`)
	fmt.Println(strings.Repeat("=", 60))
}

func selectInputFile() (string, error) {
	return dialog.File().Title("Select input CSV file").Filter("CSV Files", "csv").Load()
}

func main() {
	fmt.Println("Line Frame Sampling Tool (Go)")
	fmt.Printf("Version: %s\n", version)
	fmt.Println(strings.Repeat("-", 60))

	input := flag.String("input", "", "Input CSV file path")
	output := flag.String("o", "", "Output CSV file path")
	interval := flag.Float64("i", 100.0, "Sampling interval in meters")
	size := flag.Float64("s", 100.0, "Square edge length or circle radius in meters")
	shape := flag.String("sh", "square", "Frame shape: square or circle")
	noVis := flag.Bool("no-vis", false, "Disable visualization")
	lineColor := flag.String("lc", "#333333", "Line color for visualization")
	template := flag.Bool("t", false, "Print CSV template and exit")
	flag.Parse()

	if *template {
		printTemplate()
		return
	}

	isGUIMode := false
	inputFile := *input
	if inputFile == "" && flag.NArg() > 0 {
		inputFile = flag.Arg(0)
	}
	if inputFile == "" {
		isGUIMode = true
		selected, err := selectInputFile()
		if err != nil || selected == "" {
			fmt.Println("No file selected. Exit.")
			return
		}
		inputFile = selected
	}

	if *interval <= 0 {
		exitPause(1, isGUIMode, "Error: interval must be greater than 0.")
		return
	}
	if *size <= 0 {
		exitPause(1, isGUIMode, "Error: size must be greater than 0.")
		return
	}
	if *shape != "square" && *shape != "circle" {
		exitPause(1, isGUIMode, "Error: shape must be 'square' or 'circle'.")
		return
	}

	records, encodingName, err := loadCSV(inputFile)
	if err != nil {
		exitPause(1, isGUIMode, fmt.Sprintf("Error: failed to read CSV: %v", err))
		return
	}

	outputFile := *output
	if outputFile == "" {
		ext := filepath.Ext(inputFile)
		base := strings.TrimSuffix(inputFile, ext)
		outputFile = base + "_sampling_result" + ext
	}

	fmt.Printf("Processing: %s\n", inputFile)
	fmt.Printf("Detected encoding: %s\n", encodingName)
	fmt.Printf("Config: shape=%s, interval=%.2fm, size=%.2fm\n", *shape, *interval, *size)

	polylines, results, skippedRows := processData(records, *interval, *size, *shape)
	if len(results) == 0 {
		message := "No sampling result generated. Please check the input data."
		if skippedRows > 0 {
			message += fmt.Sprintf("\nSkipped invalid rows: %d", skippedRows)
		}
		exitPause(1, isGUIMode, message)
		return
	}

	outputHandle, err := os.Create(outputFile)
	if err != nil {
		exitPause(1, isGUIMode, fmt.Sprintf("Error: failed to create output CSV: %v", err))
		return
	}
	defer outputHandle.Close()

	if _, err := outputHandle.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		exitPause(1, isGUIMode, fmt.Sprintf("Error: failed to write BOM: %v", err))
		return
	}

	writer := csv.NewWriter(outputHandle)
	_ = writer.Write([]string{"original_line_index", "distance_m", "center_lon", "center_lat", "geom_wkt"})
	for _, result := range results {
		_ = writer.Write([]string{
			strconv.Itoa(result.LineID),
			fmt.Sprintf("%.2f", result.Distance),
			fmt.Sprintf("%.8f", result.Center.Lon),
			fmt.Sprintf("%.8f", result.Center.Lat),
			pointsToWKTPolygon(result.ShapePoints),
		})
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		exitPause(1, isGUIMode, fmt.Sprintf("Error: failed to write CSV: %v", err))
		return
	}

	fmt.Printf("Done. Generated %d frames.\n", len(results))
	if skippedRows > 0 {
		fmt.Printf("Skipped invalid rows: %d\n", skippedRows)
	}
	fmt.Printf("Output: %s\n", outputFile)

	if !*noVis {
		runVisualization(polylines, results, *lineColor)
	}

	exitPause(0, isGUIMode, "")
}

func exitPause(code int, isGUI bool, message string) {
	if message != "" {
		fmt.Println(message)
	}
	if isGUI {
		fmt.Println("\nPress Enter to exit...")
		_, _ = bufio.NewReader(os.Stdin).ReadBytes('\n')
	}
	if code != 0 {
		os.Exit(code)
	}
}
