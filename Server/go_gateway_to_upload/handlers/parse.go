package handlers

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"ricovr/gateway/models"
)

// -- Helper decoders --

func decodeStatusWord(sw uint16) string {
	fatalError := (sw >> 15) & 0x01
	motorError := (sw >> 14) & 0x01
	passthrough := (sw >> 13) & 0x01
	stm32Param := (sw >> 12) & 0x01
	soundEnable := (sw >> 11) & 0x01
	brightness := (sw >> 8) & 0x03
	postFailCnt := (sw >> 5) & 0x07

	var flags []string
	if fatalError != 0 {
		flags = append(flags, "FATAL_ERROR")
	}
	if motorError != 0 {
		flags = append(flags, "MOTOR_ERROR")
	}
	if passthrough != 0 {
		flags = append(flags, "PASSTHROUGH")
	}
	if stm32Param != 0 {
		flags = append(flags, "STM32_LOGGED")
	}
	if soundEnable != 0 {
		flags = append(flags, "SOUND_DISABLED")
	}

	brightMap := map[uint16]string{0: "High", 1: "Medium", 2: "Low", 3: "Reserved"}
	bStr, ok := brightMap[brightness]
	if !ok {
		bStr = "Unknown"
	}
	flags = append(flags, "Brightness:"+bStr)

	if postFailCnt > 0 {
		flags = append(flags, fmt.Sprintf("PostFailCount:%d", postFailCnt))
	}

	return fmt.Sprintf("SW(0x%04X): [%s]", sw, strings.Join(flags, ", "))
}

func decodePostfailByte(val byte) string {
	var fails []string
	if (val>>7)&0x01 != 0 {
		fails = append(fails, "STM32_PU_FAIL")
	}
	if (val>>6)&0x01 != 0 {
		fails = append(fails, "ACT_FAIL")
	}
	if (val>>5)&0x01 != 0 {
		fails = append(fails, "ACCEL_FAIL")
	}
	if (val>>4)&0x01 != 0 {
		fails = append(fails, "PAYLOAD_FAIL")
	}
	if (val>>3)&0x01 != 0 {
		fails = append(fails, "MOTOR_FAIL")
	}
	if len(fails) == 0 {
		return "POST Passed"
	}
	return "POST Failed: " + strings.Join(fails, ", ")
}

func decodeBatteryByte(val byte) string {
	isCharging := ((val >> 7) & 0x01) == 0
	pctIndex := (val >> 2) & 0x1F
	percentage := int(pctIndex) * 5
	status := "Battery"
	if isCharging {
		status = "Charging"
	}
	if pctIndex <= 20 {
		return fmt.Sprintf("%s %d%%", status, percentage)
	}
	return fmt.Sprintf("%s (Unknown)", status)
}

type deviceLogEntry struct {
	Key string `json:"key"`
	Val string `json:"val"`
}

func parseDeviceLogEntry(logType byte, payload []byte) string {
	switch logType {
	case 0x01: // Bootup
		if len(payload) >= 10 {
			sw := binary.LittleEndian.Uint16(payload[0:2])
			bootCnt := binary.LittleEndian.Uint16(payload[2:4])
			lastRem := binary.LittleEndian.Uint16(payload[4:6])
			testID := binary.LittleEndian.Uint16(payload[6:8])
			battRaw := binary.LittleEndian.Uint16(payload[8:10])
			return fmt.Sprintf("Bootup: %s | BootCnt:%d, LastDel:%d, TestID:%d, Batt:0x%04X",
				decodeStatusWord(sw), bootCnt, lastRem, testID, battRaw)
		}
	case 0x02: // Splash Screen
		if len(payload) > 0 {
			return "Splash Screen: " + decodePostfailByte(payload[0])
		}
		return "Splash Screen: Malformed"
	case 0x03: // Memory
		if len(payload) >= 3 {
			val := binary.LittleEndian.Uint16(payload[0:2])
			typeByte := payload[2]
			if typeByte == 0x00 {
				return fmt.Sprintf("Memory: Free %d KB", val)
			}
			return fmt.Sprintf("Memory: Del TestID %d", val)
		}
	case 0x04: // RunTest
		if len(payload) >= 2 {
			testID := binary.LittleEndian.Uint16(payload[0:2])
			return fmt.Sprintf("RunTest: ID %d", testID)
		}
	case 0x05: // Settings
		if len(payload) >= 3 {
			swVal := binary.LittleEndian.Uint16(payload[0:2])
			actStr := "Exit"
			if payload[2] == 0x00 {
				actStr = "Enter"
			}
			return fmt.Sprintf("Settings %s: %s", actStr, decodeStatusWord(swVal))
		}
	case 0x06: // FileVault
		if len(payload) > 0 {
			if payload[0] == 0x00 {
				return "FileVault: Enter"
			}
			return "FileVault: Exit"
		}
		return "FileVault: Malformed"
	case 0x07: // Battery
		if len(payload) > 0 {
			return "Power Update: " + decodeBatteryByte(payload[0])
		}
		return "Power Update: Malformed"
	case 0x08: // Result
		if len(payload) >= 13 {
			resCode := payload[0]
			t0 := math.Float32frombits(binary.LittleEndian.Uint32(payload[1:5]))
			t10 := math.Float32frombits(binary.LittleEndian.Uint32(payload[5:9]))
			absorb := math.Float32frombits(binary.LittleEndian.Uint32(payload[9:13]))
			resMap := map[byte]string{
				0: "Not Displayed", 1: "Negative", 2: "Non-Negative",
				3: "Incomplete", 4: "Invalid",
			}
			resStr, ok := resMap[resCode]
			if !ok {
				resStr = "Unknown"
			}
			return fmt.Sprintf("Result: %s | T0:%.4f, T10:%.4f, Abs:%.4f", resStr, t0, t10, absorb)
		}
	case 0x09: // Config (Variable)
		if len(payload) < 4 {
			return "Config Payload: Malformed"
		}
		state := payload[0]
		checksum := binary.LittleEndian.Uint16(payload[len(payload)-2:])
		delay := payload[len(payload)-3]
		var hexParts []string
		for _, b := range payload[1 : len(payload)-3] {
			hexParts = append(hexParts, fmt.Sprintf("%02X", b))
		}
		return fmt.Sprintf("Config Row: State %d | Delay %dms | Chk 0x%04X | Data [%s]",
			state, delay, checksum, strings.Join(hexParts, " "))
	case 0x0F: // Shutdown
		if len(payload) > 0 {
			reasons := map[byte]string{
				0: "Unknown", 1: "Long Press", 2: "Settings Menu", 3: "Timeout",
			}
			r, ok := reasons[payload[0]&0x03]
			if !ok {
				r = "Unknown"
			}
			return "Shutdown: " + r
		}
		return "Shutdown: Malformed"
	}
	return fmt.Sprintf("Unknown Opcode 0x%02X", logType)
}

// ParseDeviceLog handles POST /api/parse/device-log?mac_id=X
// Verifies the caller has ACL access to mac_id before parsing.
// Body: raw UTF-8 bytes of the hex log file (Content-Type: application/octet-stream)
// Response: [{"key": "1234 ms", "val": "Bootup: ..."}]
func ParseDeviceLog(store *models.ACLStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		macID := c.Query("mac_id")
		if macID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "mac_id query param required"})
			return
		}
		if _, ok := checkOrgAccess(c, store, macID); !ok {
			return
		}

		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
			return
		}

		// Strip whitespace and newlines from hex text
		clean := strings.ReplaceAll(string(body), "\n", "")
		clean = strings.ReplaceAll(clean, "\r", "")
		clean = strings.ReplaceAll(clean, " ", "")

		if clean == "" {
			c.JSON(http.StatusOK, []deviceLogEntry{})
			return
		}

		data, err := hex.DecodeString(clean)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid hex data"})
			return
		}

		var entries []deviceLogEntry
		offset := 0
		total := len(data)

		for offset < total {
			if data[offset] != 0x5A {
				offset++
				continue
			}
			if offset+8 > total {
				break
			}

			// Header: start(1) + logType(1) + timestamp uint32(4) + payloadLen uint16(2) = 8 bytes
			logType := data[offset+1]
			timestamp := binary.LittleEndian.Uint32(data[offset+2 : offset+6])
			payloadLen := int(binary.LittleEndian.Uint16(data[offset+6 : offset+8]))

			nextOffset := offset + 8 + payloadLen
			if nextOffset > total {
				entries = append(entries, deviceLogEntry{
					Key: fmt.Sprintf("%d ms", timestamp),
					Val: fmt.Sprintf("Error: Truncated payload for Opcode 0x%02X", logType),
				})
				break
			}

			payload := data[offset+8 : nextOffset]
			desc := parseDeviceLogEntry(logType, payload)
			entries = append(entries, deviceLogEntry{
				Key: fmt.Sprintf("%d ms", timestamp),
				Val: desc,
			})
			offset = nextOffset
		}

		if entries == nil {
			entries = []deviceLogEntry{}
		}
		c.JSON(http.StatusOK, entries)
	}
}

// Series describes a single line/scatter series on the plot.
type Series struct {
	Label       string    `json:"label"`
	Data        []float64 `json:"data"`
	Color       string    `json:"color"`
	LineStyle   string    `json:"line_style"`
	LineWidth   float64   `json:"line_width"`
	MarkerStyle string    `json:"marker_style"`
	MarkerSize  float64   `json:"marker_size"`
	YAxis       int       `json:"y_axis"`
}

// Axis describes one axis (x or y).
type Axis struct {
	Label string   `json:"label"`
	Unit  string   `json:"unit,omitempty"`
	Min   *float64 `json:"min,omitempty"`
	Max   *float64 `json:"max,omitempty"`
}

// Legend describes the chart legend configuration.
type Legend struct {
	Visible  bool   `json:"visible"`
	Position string `json:"position"` // "bottom" | "top" | "left" | "right"
}

// PlotSpec is the complete plot descriptor returned by ParseTestLog.
// XValues is always the time axis (seconds if frequency known, sample index otherwise).
// The Python client passes this through unchanged; QML renders it directly.
type PlotSpec struct {
	Success   bool      `json:"success"`
	Title     string    `json:"title"`
	Legend    Legend    `json:"legend"`
	XAxis     Axis      `json:"x_axis"`
	YAxes     []Axis    `json:"y_axes"`
	Series    []Series  `json:"series"`
	XValues   []float64 `json:"x_values"`           // always present — time column
	Frequency *float64  `json:"frequency,omitempty"` // Hz; nil when unknown
}

// ParseTestLog handles POST /api/parse/test-log?mac_id=X
// Verifies the caller has ACL access to mac_id before parsing.
// Body: raw UTF-8 bytes of the hex test log file (Content-Type: application/octet-stream)
// Response: PlotSpec JSON with full plot descriptor (title, axes, series, x_values)
func ParseTestLog(store *models.ACLStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		macID := c.Query("mac_id")
		if macID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "mac_id query param required"})
			return
		}
		if _, ok := checkOrgAccess(c, store, macID); !ok {
			return
		}

		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
			return
		}

		lines := strings.Split(string(body), "\n")

		var frequency *float64
		var dataPoints []int
		first10sPacketFound := false

		for _, line := range lines {
			line = strings.TrimSpace(line)
			if len(line) < 28 {
				continue
			}

			command := line[0:4]
			// statusdword: rearrange 8 hex chars at positions 4-11 into little-endian order
			statusdword := strings.ToLower(line[10:12] + line[8:10] + line[6:8] + line[4:6])

			if command != "5521" {
				continue
			}

			// Parse payload length (little-endian hex from chars 24-27)
			payloadLenHex := line[26:28] + line[24:26]
			var payloadLen int
			if _, e := fmt.Sscanf(payloadLenHex, "%x", &payloadLen); e != nil {
				c.JSON(http.StatusOK, PlotSpec{Success: false, Series: []Series{}})
				return
			}
			numPoints := payloadLen / 2

			var currentPacketValid bool
			if statusdword == "5130fff4" && !first10sPacketFound {
				freq := float64(numPoints) / 10.0
				frequency = &freq
				first10sPacketFound = true
				currentPacketValid = true
			} else if statusdword == "5130fff4" {
				currentPacketValid = true
			} else if statusdword == "4130fff4" {
				currentPacketValid = true
			}

			if currentPacketValid && len(line) > 36 {
				payloadHex := line[28 : len(line)-8]
				for i := 0; i+4 <= len(payloadHex); i += 4 {
					chunk := payloadHex[i : i+4]
					pointHex := chunk[2:4] + chunk[0:2]
					var point int
					if _, e := fmt.Sscanf(pointHex, "%x", &point); e == nil {
						dataPoints = append(dataPoints, point)
					}
				}
			}
		}

		success := len(dataPoints) > 0
		if dataPoints == nil {
			dataPoints = []int{}
		}

		// Convert int data points to float64 for PlotSpec
		floatData := make([]float64, len(dataPoints))
		for i, v := range dataPoints {
			floatData[i] = float64(v)
		}

		// XValues — Reverting to Python's Milliseconds scale
		xValues := make([]float64, len(dataPoints))
		xUnit := "samples"
		seriesLabel := "Frequency: Unknown"

		if frequency != nil && *frequency > 0 {
			// Python formula: (i / frequency) * 1000
			multiplier := 1000.0 / *frequency
			for i := range xValues {
				xValues[i] = float64(i) * multiplier
			}
			xUnit = "ms"
			// Matches: self.legend.addItem(self.hf_curve, f"Frequency: {frequency} Hz")
			seriesLabel = fmt.Sprintf("Frequency: %v Hz", *frequency) 
		} else {
			for i := range xValues {
				xValues[i] = float64(i)
			}
		}

		spec := PlotSpec{
			Success:   success,
			Frequency: frequency,
			Title:     "ADC Values", // Restored from "PPG Test Signal"
			Legend:    Legend{
				Visible:  true,
				Position: "top-right", // Restored from "bottom" (matches anchor 1,0)
			},
			XAxis:   Axis{
				Label: "Time (" + xUnit + ")",
			},
			YAxes:   []Axis{{
				Label: "ADC",
			}},
			Series: []Series{
				{
					Label:       seriesLabel,
					Data:        floatData,
					Color:       "#4698CB", // Restored custom light blue
					LineStyle:   "solid",
					LineWidth:   2.0,       // Restored from 1.5
					MarkerStyle: "none",
					MarkerSize:  0,
					YAxis:       0,
				},
			},
			XValues: xValues,
		}
		c.JSON(http.StatusOK, spec)
	}
}