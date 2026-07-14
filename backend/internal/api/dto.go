package api

import (
	"strings"

	"github.com/gin-gonic/gin"

	"dps150-web/backend/internal/device"
	"dps150-web/backend/internal/device/protocol"
)

// errorDTO is the error envelope of the API contract:
// {"error": {"code": "...", "message": "..."}}.
type errorDTO struct {
	Error errorInfoDTO `json:"error"`
}

type errorInfoDTO struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeError(c *gin.Context, status int, code, message string) {
	c.JSON(status, errorDTO{Error: errorInfoDTO{Code: code, Message: message}})
}

// deviceDTO is the GET /api/v1/device response and the data of WS "state"
// messages.
type deviceDTO struct {
	Connected bool      `json:"connected"`
	Transport string    `json:"transport"`
	Info      *infoDTO  `json:"info"`
	State     *stateDTO `json:"state"`
}

type infoDTO struct {
	Model    string `json:"model"`
	Hardware string `json:"hardware"`
	Firmware string `json:"firmware"`
}

type stateDTO struct {
	OutputOn     bool           `json:"outputOn"`
	Mode         string         `json:"mode"`
	Protection   string         `json:"protection"`
	Setpoints    setpointsDTO   `json:"setpoints"`
	Measured     measuredDTO    `json:"measured"`
	InputVoltage float64        `json:"inputVoltage"`
	Temperature  float64        `json:"temperature"`
	Limits       limitsDTO      `json:"limits"`
	Metering     meteringDTO    `json:"metering"`
	Protections  protectionsDTO `json:"protections"`
	Brightness   int            `json:"brightness"`
	Volume       int            `json:"volume"`
	UpdatedAt    int64          `json:"updatedAt"`
}

type setpointsDTO struct {
	Voltage float64 `json:"voltage"`
	Current float64 `json:"current"`
}

type measuredDTO struct {
	Voltage float64 `json:"voltage"`
	Current float64 `json:"current"`
	Power   float64 `json:"power"`
}

type limitsDTO struct {
	MaxVoltage float64 `json:"maxVoltage"`
	MaxCurrent float64 `json:"maxCurrent"`
}

type meteringDTO struct {
	CapacityAh float64 `json:"capacityAh"`
	EnergyWh   float64 `json:"energyWh"`
}

type protectionsDTO struct {
	OVP float64 `json:"ovp"`
	OCP float64 `json:"ocp"`
	OPP float64 `json:"opp"`
	OTP float64 `json:"otp"`
	LVP float64 `json:"lvp"`
}

type outputDTO struct {
	On bool `json:"on"`
}

// telemetryDTO is the data of WS "telemetry" messages.
type telemetryDTO struct {
	Measured     measuredDTO `json:"measured"`
	InputVoltage float64     `json:"inputVoltage"`
	Temperature  float64     `json:"temperature"`
	Mode         string      `json:"mode"`
	Protection   string      `json:"protection"`
	OutputOn     bool        `json:"outputOn"`
	Metering     meteringDTO `json:"metering"`
	TS           int64       `json:"ts"`
}

// statusDTO is the data of WS "status" messages.
type statusDTO struct {
	Connected bool   `json:"connected"`
	Transport string `json:"transport"`
}

func deviceJSON(s device.Snapshot) deviceDTO {
	d := deviceDTO{
		Connected: s.Connected,
		Transport: s.Transport,
	}
	if s.Info != nil {
		d.Info = &infoDTO{
			Model:    s.Info.Model,
			Hardware: s.Info.Hardware,
			Firmware: s.Info.Firmware,
		}
	}
	if s.State != nil {
		st := s.State
		d.State = &stateDTO{
			OutputOn:     st.OutputOn,
			Mode:         modeJSON(st.Mode),
			Protection:   protectionJSON(st.Protection),
			Setpoints:    setpointsDTO{Voltage: st.SetVoltage, Current: st.SetCurrent},
			Measured:     measuredDTO{Voltage: st.Voltage, Current: st.Current, Power: st.Power},
			InputVoltage: st.InputVoltage,
			Temperature:  st.Temperature,
			Limits:       limitsDTO{MaxVoltage: st.MaxVoltage, MaxCurrent: st.MaxCurrent},
			Metering:     meteringDTO{CapacityAh: st.CapacityAh, EnergyWh: st.EnergyWh},
			Protections:  protectionsDTO{OVP: st.OVP, OCP: st.OCP, OPP: st.OPP, OTP: st.OTP, LVP: st.LVP},
			Brightness:   int(st.Brightness),
			Volume:       int(st.Volume),
			UpdatedAt:    st.UpdatedAt.UnixMilli(),
		}
	}
	return d
}

func telemetryJSON(t device.Telemetry) telemetryDTO {
	return telemetryDTO{
		Measured:     measuredDTO{Voltage: t.Voltage, Current: t.Current, Power: t.Power},
		InputVoltage: t.InputVoltage,
		Temperature:  t.Temperature,
		Mode:         modeJSON(t.Mode),
		Protection:   protectionJSON(t.Protection),
		OutputOn:     t.OutputOn,
		Metering:     meteringDTO{CapacityAh: t.CapacityAh, EnergyWh: t.EnergyWh},
		TS:           t.TS.UnixMilli(),
	}
}

// modeJSON maps a regulation mode to the contract's "cc" | "cv".
func modeJSON(m protocol.Mode) string {
	if m == protocol.ModeCC {
		return "cc"
	}
	return "cv"
}

// protectionJSON maps a protection state to the contract's
// "ok" | "ovp" | "ocp" | "opp" | "otp" | "lvp" | "rep".
func protectionJSON(p protocol.Protection) string {
	return strings.ToLower(p.String())
}
