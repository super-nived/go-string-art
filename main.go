package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"math"
	"net/http"
	"os"
)

//go:embed page.html
var page string

// Structs
type Coord struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type StringArtResponse struct {
	Sequence  []int   `json:"sequence"`
	PinCount  int     `json:"pin_count"`
	LineCount int     `json:"line_count"`
	PinCoords []Coord `json:"pin_coordinates"`
	ImageSize int     `json:"image_size"`
	Status    string  `json:"status"`
	Message   string  `json:"message,omitempty"`
}

type ErrorResponse struct {
	Error   string `json:"error"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// Constants
const PINS = 240
const MIN_DISTANCE = 30
const MAX_LINES = 4000
const LINE_WEIGHT = 8

// Global variables for processing
var IMG_SIZE = 500
var IMG_SIZE_FL = float64(500)
var IMG_SIZE_SQ = 250000

func init() {
	image.RegisterFormat("jpeg", "jpeg", jpeg.Decode, jpeg.DecodeConfig)
	image.RegisterFormat("png", "png", png.Decode, png.DecodeConfig)
}

func main() {
	// CORS middleware
	http.HandleFunc("/", corsMiddleware(serveHello))
	http.HandleFunc("/api/string-art", corsMiddleware(stringArtHandler))
	http.HandleFunc("/api/health", corsMiddleware(healthHandler))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Println("Server listening on http://localhost:" + port)
	log.Println("Endpoints:")
	log.Println("  GET  / - Home page")
	log.Println("  POST /api/string-art - Upload image and get string art sequence")
	log.Println("  GET  /api/health - Health check")

	err := http.ListenAndServe(":"+port, nil)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal("Error starting server:", err)
	}
}

func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func serveHello(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, page)
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	response := map[string]string{
		"status":  "healthy",
		"service": "string-art-api",
	}
	json.NewEncoder(w).Encode(response)
}

func stringArtHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Parse multipart form
	err := r.ParseMultipartForm(10 << 20) // 10 MB max
	if err != nil {
		sendErrorResponse(w, "Failed to parse form", err.Error(), http.StatusBadRequest)
		return
	}

	// Get uploaded file
	file, header, err := r.FormFile("image")
	if err != nil {
		sendErrorResponse(w, "No image file provided", err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Get shape
	shape := r.FormValue("shape")
	if shape == "" {
		shape = "circle" // Default shape
	}

	// Validate file type
	contentType := header.Header.Get("Content-Type")
	if contentType != "image/jpeg" && contentType != "image/png" && contentType != "image/jpg" {
		sendErrorResponse(w, "Invalid file type", "Only JPEG and PNG images are supported", http.StatusBadRequest)
		return
	}

	// Process the image
	result, err := processStringArt(file, shape)
	if err != nil {
		sendErrorResponse(w, "Failed to process image", err.Error(), http.StatusInternalServerError)
		return
	}

	// Send successful response
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(result)
}

func sendErrorResponse(w http.ResponseWriter, error, message string, statusCode int) {
	w.WriteHeader(statusCode)
	response := ErrorResponse{
		Error:   error,
		Status:  "error",
		Message: message,
	}
	json.NewEncoder(w).Encode(response)
}

func processStringArt(file io.Reader, shape string) (*StringArtResponse, error) {
	// Import image and get pixel array
	sourceImage, err := getPixels(file)
	if err != nil {
		return nil, fmt.Errorf("failed to process image pixels: %v", err)
	}

	// Calculate pin coordinates
	pinCoords, err := calculatePinCoords(shape)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate pin coordinates: %v", err)
	}

	// Precalculate all potential lines
	lineCacheY, lineCacheX := precalculateAllPotentialLines(pinCoords)

	// Calculate the optimal line sequence
	sequence := calculateLines(sourceImage, lineCacheY, lineCacheX)

	// Prepare response
	response := &StringArtResponse{
		Sequence:  sequence,
		PinCount:  PINS,
		LineCount: len(sequence),
		PinCoords: pinCoords,
		ImageSize: IMG_SIZE,
		Status:    "success",
	}

	return response, nil
}

func getPixels(file io.Reader) ([]float64, error) {
	img, _, err := image.Decode(file)
	if err != nil {
		return nil, err
	}

	bounds := img.Bounds()
	width, height := bounds.Max.X, bounds.Max.Y
	IMG_SIZE = width
	IMG_SIZE_FL = float64(IMG_SIZE)
	IMG_SIZE_SQ = IMG_SIZE * IMG_SIZE

	var pixels []float64
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			pixels = append(pixels, rgbaToPixel(img.At(x, y).RGBA()))
		}
	}

	return pixels, nil
}

func rgbaToPixel(r uint32, g uint32, b uint32, a uint32) float64 {
	return float64(r / 257)
}

func calculatePinCoords(shape string) ([]Coord, error) {
	switch shape {
	case "circle":
		return calculateCirclePinCoords(), nil
	case "square":
		return calculateSquarePinCoords(), nil
	case "heart":
		return calculateHeartPinCoords(), nil
	case "triangle":
		return calculateTrianglePinCoords(), nil
	default:
		return nil, fmt.Errorf("invalid shape: %s", shape)
	}
}

func calculateCirclePinCoords() []Coord {
	pinCoords := make([]Coord, PINS)
	center := float64(IMG_SIZE / 2)
	radius := float64(IMG_SIZE/2 - 1)

	for i := 0; i < PINS; i++ {
		angle := 2 * math.Pi * float64(i) / float64(PINS)
		pinCoords[i] = Coord{
			X: math.Floor(center + radius*math.Cos(angle)),
			Y: math.Floor(center + radius*math.Sin(angle)),
		}
	}

	return pinCoords
}

func calculateSquarePinCoords() []Coord {
	pinCoords := make([]Coord, PINS)
	border := float64(IMG_SIZE - 1)
	pinsPerSide := PINS / 4

	for i := 0; i < PINS; i++ {
		side := i / pinsPerSide
		pinInSide := i % pinsPerSide
		progress := float64(pinInSide) / float64(pinsPerSide)

		switch side {
		case 0: // Top
			pinCoords[i] = Coord{X: progress * border, Y: 0}
		case 1: // Right
			pinCoords[i] = Coord{X: border, Y: progress * border}
		case 2: // Bottom
			pinCoords[i] = Coord{X: border - (progress * border), Y: border}
		case 3: // Left
			pinCoords[i] = Coord{X: 0, Y: border - (progress * border)}
		}
	}

	return pinCoords
}

func calculateHeartPinCoords() []Coord {
	pinCoords := make([]Coord, PINS)
	center := float64(IMG_SIZE / 2)
	scale := float64(IMG_SIZE / 4)

	for i := 0; i < PINS; i++ {
		t := 2 * math.Pi * float64(i) / float64(PINS)
		x := scale * (16*math.Pow(math.Sin(t), 3))
		y := -scale * (13*math.Cos(t) - 5*math.Cos(2*t) - 2*math.Cos(3*t) - math.Cos(4*t))

		pinCoords[i] = Coord{
			X: math.Floor(x + center),
			Y: math.Floor(y + center),
		}
	}
	return pinCoords
}

func calculateTrianglePinCoords() []Coord {
	pinCoords := make([]Coord, PINS)
	sideLength := float64(IMG_SIZE - 10) // A bit smaller to fit in canvas

	// Vertices of an equilateral triangle
	// Top vertex
	v1 := Coord{X: float64(IMG_SIZE)/2, Y: (float64(IMG_SIZE) - sideLength*math.Sqrt(3)/2) / 2}
	// Bottom-right vertex
	v2 := Coord{X: (float64(IMG_SIZE) + sideLength) / 2, Y: (float64(IMG_SIZE) + sideLength*math.Sqrt(3)/2) / 2}
	// Bottom-left vertex
	v3 := Coord{X: (float64(IMG_SIZE) - sideLength) / 2, Y: (float64(IMG_SIZE) + sideLength*math.Sqrt(3)/2) / 2}

	pinsPerSide := PINS / 3

	for i := 0; i < PINS; i++ {
		side := i / pinsPerSide
		pinInSide := i % pinsPerSide
		progress := float64(pinInSide) / float64(pinsPerSide)

		switch side {
		case 0: // From v1 to v2
			pinCoords[i] = Coord{
				X: math.Floor(v1.X + progress*(v2.X-v1.X)),
				Y: math.Floor(v1.Y + progress*(v2.Y-v1.Y)),
			}
		case 1: // From v2 to v3
			pinCoords[i] = Coord{
				X: math.Floor(v2.X + progress*(v3.X-v2.X)),
				Y: math.Floor(v2.Y + progress*(v3.Y-v2.Y)),
			}
		case 2: // From v3 to v1
			pinCoords[i] = Coord{
				X: math.Floor(v3.X + progress*(v1.X-v3.X)),
				Y: math.Floor(v3.Y + progress*(v1.Y-v3.Y)),
			}
		}
	}
	return pinCoords
}

func precalculateAllPotentialLines(pinCoords []Coord) ([][]float64, [][]float64) {
	lineCacheY := make([][]float64, PINS*PINS)
	lineCacheX := make([][]float64, PINS*PINS)

	for i := 0; i < PINS; i++ {
		for j := i + MIN_DISTANCE; j < PINS; j++ {
			x0 := pinCoords[i].X
			y0 := pinCoords[i].Y
			x1 := pinCoords[j].X
			y1 := pinCoords[j].Y

			d := math.Floor(math.Sqrt((x1-x0)*(x1-x0) + (y1-y0)*(y1-y0)))
			xs := linspace(x0, x1, int(d))
			ys := linspace(y0, y1, int(d))

			// Round to integers
			for k := range xs {
				xs[k] = float64(int(xs[k]))
				ys[k] = float64(int(ys[k]))
			}

			lineCacheY[j*PINS+i] = ys
			lineCacheY[i*PINS+j] = ys
			lineCacheX[j*PINS+i] = xs
			lineCacheX[i*PINS+j] = xs
		}
	}

	return lineCacheY, lineCacheX
}

func linspace(start, stop float64, num int) []float64 {
	if num <= 1 {
		return []float64{start}
	}

	result := make([]float64, num)
	step := (stop - start) / float64(num-1)

	for i := 0; i < num; i++ {
		result[i] = start + float64(i)*step
	}

	return result
}

func calculateLines(sourceImage []float64, lineCacheY, lineCacheX [][]float64) []int {
	// Create error image
	errorImage := make([]float64, len(sourceImage))
	for i := range sourceImage {
		errorImage[i] = 255.0 - sourceImage[i]
	}

	var lineSequence []int
	currentPin := 0
	lastPins := make([]int, 20)

	for i := 0; i < MAX_LINES; i++ {
		bestPin := -1
		maxErr := float64(0)
		bestIndex := 0

		for offset := MIN_DISTANCE; offset < PINS-MIN_DISTANCE; offset++ {
			testPin := (currentPin + offset) % PINS
			if contains(lastPins, testPin) {
				continue
			}

			innerIndex := testPin*PINS + currentPin
			if innerIndex >= len(lineCacheY) || lineCacheY[innerIndex] == nil {
				continue
			}

			lineErr := getLineErr(errorImage, lineCacheY[innerIndex], lineCacheX[innerIndex])
			if lineErr > maxErr {
				maxErr = lineErr
				bestPin = testPin
				bestIndex = innerIndex
			}
		}

		if bestPin == -1 {
			break
		}

		lineSequence = append(lineSequence, bestPin)

		// Update error image
		coords1 := lineCacheY[bestIndex]
		coords2 := lineCacheX[bestIndex]
		for j := range coords1 {
			v := int((coords1[j] * IMG_SIZE_FL) + coords2[j])
			if v >= 0 && v < len(errorImage) {
				errorImage[v] = errorImage[v] - LINE_WEIGHT
			}
		}

		// Update last pins
		if len(lastPins) >= 20 {
			lastPins = append(lastPins[1:], bestPin)
		} else {
			lastPins = append(lastPins, bestPin)
		}
		currentPin = bestPin
	}

	return lineSequence
}

func getLineErr(err, coords1, coords2 []float64) float64 {
	sum := float64(0)
	for i := 0; i < len(coords1); i++ {
		index := int((coords1[i] * IMG_SIZE_FL) + coords2[i])
		if index >= 0 && index < len(err) {
			sum += err[index]
		}
	}
	return sum
}

func contains(arr []int, num int) bool {
	for _, val := range arr {
		if val == num {
			return true
		}
	}
	return false
}
