// Package analysis performs offline NTP exchange accounting: request /
// response pairing, era disambiguation of the 32-bit seconds field,
// fixed-point delay/offset/dispersion math, clock-filter ordering and
// deterministic peer selection.
package analysis

import (
	"eraledger/internal/ntp"
)

// Leap policy identifiers select how leap-second markers are treated.
const (
	LeapRaw   = "raw"   // ignore leap markers entirely
	LeapAvoid = "avoid" // reject samples whose server time lies inside the inserted second
	LeapStep  = "step"  // map the inserted second onto the pre-leap scale
)

// Reject codes record why an exchange was excluded from selection.
const (
	RejectShortPacket      = "short_packet"
	RejectBadVersion       = "bad_version"
	RejectNegativeFP       = "negative_fixedpoint"
	RejectBadPrecision     = "bad_precision"
	RejectKissODeath       = "kiss_o_death"
	RejectLI3              = "li3_unsynchronized"
	RejectUnsolicited      = "unsolicited_response"
	RejectOriginMismatch   = "origin_mismatch"
	RejectDuplicateResp    = "duplicate_response"
	RejectNegativeDelay    = "negative_delay"
	RejectLeapInsideSecond = "leap_inside_inserted_second"
	RejectEraAmbiguous     = "era_ambiguous"
)

// ClockSource identifies the clock that stamped a local capture time.
type ClockSource struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"` // e.g. gps-pps, system-rtc, unknown
	Trusted bool   `json:"trusted"`
	Note    string `json:"note"`
}

// CaptureInput is one imported packet plus the local observation time.
type CaptureInput struct {
	Seq        int    `json:"seq"`
	Client     string `json:"client"`
	Server     string `json:"server"`
	Packet     []byte `json:"packet"`
	LocalNS    int64  `json:"local_ns"`    // unix nanoseconds of local observation
	LocalClock string `json:"local_clock"` // ClockSource id
	Dir        string `json:"dir"`         // "send" or "recv"
}

// LeapBound describes an announced leap boundary in NTP seconds (the first
// second of the inserted/removed day, e.g. 1483228800 for 2017-01-01).
type LeapBound struct {
	NTPSeconds uint32 `json:"ntp_seconds"`
	Kind       uint8  `json:"kind"` // ntp.LILastMin61 or ntp.LILastMin59
}

// Capture is the full import for one ledger.
type Capture struct {
	Sources []ClockSource  `json:"sources"`
	Leaps   []LeapBound    `json:"leaps"`
	Records []CaptureInput `json:"records"`
}

// Evidence is one human-readable audit item with a stable code.
type Evidence struct {
	ID     int    `json:"id"`
	Scope  string `json:"scope"` // "exchange" | "peer"
	Ref    string `json:"ref"`   // exchange id or peer id
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

// FourTimestamps retains the resolved four NTP timestamps of an exchange.
type FourTimestamps struct {
	Origin      ntp.ExtendedStamp `json:"-"`
	Receive     ntp.ExtendedStamp `json:"-"`
	Transmit    ntp.ExtendedStamp `json:"-"`
	Destination ntp.ExtendedStamp `json:"-"`
}

// EraCandidate is one feasible era assignment for an exchange.
type EraCandidate struct {
	Eras         [4]uint32 `json:"eras"` // t1..t4 era numbers
	BaseEra      uint32    `json:"base_era"`
	ShiftCost    int       `json:"shift_cost"`
	DelayTick32  string    `json:"delay_t32"`
	OffsetTick32 string    `json:"offset_t32"`
	Valid        bool      `json:"valid"`
	Reason       string    `json:"reason,omitempty"`
	Chosen       bool      `json:"chosen"`
	Note         string    `json:"note,omitempty"`
}

// Exchange is a paired request/response (or a rejected response).
type Exchange struct {
	ID         string `json:"id"`
	Client     string `json:"client"`
	Peer       string `json:"peer"`
	SendSeq    int    `json:"send_seq"`
	RecvSeq    int    `json:"recv_seq"`
	LocalClock string `json:"local_clock"`
	SendNS     int64  `json:"send_ns"`
	RecvNS     int64  `json:"recv_ns"`

	Request  *ntp.Packet `json:"-"`
	Response *ntp.Packet `json:"-"`

	RawOrigin      uint64 `json:"raw_origin"`
	RawReceive     uint64 `json:"raw_receive"`
	RawTransmit    uint64 `json:"raw_transmit"`
	RawReference   uint64 `json:"raw_reference"`
	RawReqTransmit uint64 `json:"raw_request_transmit"`

	Rejected   bool   `json:"rejected"`
	RejectCode string `json:"reject_code,omitempty"`
	RejectNote string `json:"reject_note,omitempty"`

	Candidates []EraCandidate `json:"candidates"`
	EraNote    string         `json:"era_note"`

	// Fields populated for the chosen candidate only.
	ChosenCandidate int   `json:"chosen_candidate"`
	DelayTick32     int64 `json:"delay_t32"`
	OffsetTick32    int64 `json:"offset_t32"`
	DelayNS         int64 `json:"delay_ns"`
	OffsetNS        int64 `json:"offset_ns"`

	RootDelayTick64    uint64 `json:"root_delay_t64"`
	RootDispTick64     uint64 `json:"root_disp_t64"`
	ServerPrecTick64   uint64 `json:"server_prec_t64"`
	ClientPrecTick64   uint64 `json:"client_prec_t64"`
	AgeTick32          int64  `json:"age_t32"`
	PhiTick64Num       string `json:"phi_t64_num"` // rational phi*age numerator (tick64)
	PhiTick64Den       string `json:"phi_t64_den"`
	RhoTick64          uint64 `json:"rho_t64"`
	RootDistanceTick64 uint64 `json:"root_distance_t64"`

	Stratum      uint8  `json:"stratum"`
	LI           uint8  `json:"li"`
	Poll         int8   `json:"poll"`
	ReferenceID  uint32 `json:"reference_id"`
	ReferenceRaw uint64 `json:"reference_raw"`
	ReferenceEra uint32 `json:"reference_era"`

	DuplicateTransmit bool `json:"duplicate_transmit"`
	DuplicateResponse bool `json:"duplicate_response"`
	Late              bool `json:"late"`
	LeapAdjusted      bool `json:"leap_adjusted"`
	LeapInside        bool `json:"leap_inside"`
}

// FilterSample is a candidate kept in a peer's clock filter.
type FilterSample struct {
	ExchangeID   string `json:"exchange_id"`
	T3Raw        uint64 `json:"t3_raw"`
	RootDistance uint64 `json:"root_distance_t64"`
	Rho          string `json:"rho_exact"` // tick64 rational for tie-breaking
	OffsetTick32 int64  `json:"offset_t32"`
	RecvNS       int64  `json:"recv_ns"`
	Kept         bool   `json:"kept"`
	Dropped      bool   `json:"dropped"`
	DropReason   string `json:"drop_reason,omitempty"`
	Rank         int    `json:"rank"`
}

// PeerState is the maintained state for one (client, server) association.
type PeerState struct {
	ID            string         `json:"id"`
	Client        string         `json:"client"`
	Server        string         `json:"server"`
	Reach         uint8          `json:"reach"`
	ReachBinary   string         `json:"reach_binary"`
	Stratum       uint8          `json:"stratum"`
	ReferenceID   uint32         `json:"reference_id"`
	EraCandidates []uint32       `json:"era_candidates"`
	Filter        []FilterSample `json:"filter"`
	BestExchange  string         `json:"best_exchange"`
	BestDistance  uint64         `json:"best_distance_t64"`
	BestOffset    int64          `json:"best_offset_t32"`
	Incoherent    bool           `json:"incoherent"`
	Notes         []string       `json:"notes"`
	ExchangeIDs   []string       `json:"exchange_ids"`
}

// Settings controls a deterministic analysis run.
type Settings struct {
	// AnchorNS is the trusted unix-ns instant the local capture clock is
	// known to display at AnchorLocalNS. Zero means no anchor.
	AnchorNS      int64  `json:"anchor_ns"`
	AnchorLocalNS int64  `json:"anchor_local_ns"`
	AnchorClock   string `json:"anchor_clock"`
	HasAnchor     bool   `json:"has_anchor"`
	LeapPolicy    string `json:"leap_policy"`
	LocalTolNS    int64  `json:"local_tol_ns"`
	MaxOffsetNS   int64  `json:"max_offset_ns"`
	MaxFilter     int    `json:"max_filter"`
}

// Result is a fully computed analysis run.
type Result struct {
	Settings  Settings     `json:"settings"`
	Exchanges []*Exchange  `json:"exchanges"`
	Peers     []*PeerState `json:"peers"`
	Evidence  []Evidence   `json:"evidence"`
	Selected  []string     `json:"selected_peers"`
}
