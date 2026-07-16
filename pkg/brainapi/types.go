package brainapi

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
)

// Session is the body returned by POST /authentication on success.
// Two shapes can land at 201:
//   - normal login -> User + Token + Permissions populated
//   - persona required -> Inquiry populated, others zero
type Session struct {
	User        *UserRef `json:"user,omitempty"`
	Token       *Token   `json:"token,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
	Inquiry     string   `json:"inquiry,omitempty"`
}

// UserRef is the minimal user identifier embedded in session bodies.
type UserRef struct {
	ID string `json:"id"`
}

// Token holds session-token TTL info. Expiry is "seconds remaining" — not an
// absolute timestamp — per BRAIN's GET /authentication response shape.
type Token struct {
	Expiry float64 `json:"expiry"`
}

// SessionInfo is the body of GET /authentication.
type SessionInfo struct {
	User        UserRef  `json:"user"`
	Token       Token    `json:"token"`
	Permissions []string `json:"permissions"`
}

// User is the body of GET /users/self. Twenty-one top-level keys observed
// 2026-05-06; we keep the common ones strongly typed and fall back to Extra
// (json.RawMessage) for forward-compat with new fields BRAIN may add.
type User struct {
	ID           string          `json:"id"`
	Email        string          `json:"email"`
	Telephone    *string         `json:"telephone"`
	FirstName    string          `json:"firstName"`
	LastName     string          `json:"lastName"`
	FullName     string          `json:"fullName"`
	Gender       string          `json:"gender"`
	DateCreated  string          `json:"dateCreated"`
	DateVerified string          `json:"dateVerified"`
	DateApproved string          `json:"dateApproved"`
	Verified     bool            `json:"verified"`
	Approved     bool            `json:"approved"`
	Address      json.RawMessage `json:"address"`
	Education    json.RawMessage `json:"education"`
	Employment   json.RawMessage `json:"employment"`
	Recruitment  json.RawMessage `json:"recruitment"`
	Resume       json.RawMessage `json:"resume"`
	Image        json.RawMessage `json:"image"`
	Settings     json.RawMessage `json:"settings"`
	Onboarding   json.RawMessage `json:"onboarding"`
	GeniusLevel  json.RawMessage `json:"geniusLevel"`
}

// Alpha mirrors the body of GET /alphas/{id} and the items in
// GET /users/self/alphas results[].
type Alpha struct {
	ID              string           `json:"id"`
	Type            string           `json:"type"`
	Author          string           `json:"author,omitempty"`
	Settings        json.RawMessage  `json:"settings,omitempty"`
	Regular         json.RawMessage  `json:"regular,omitempty"`
	Combo           json.RawMessage  `json:"combo,omitempty"`
	Selection       json.RawMessage  `json:"selection,omitempty"`
	DateCreated     string           `json:"dateCreated,omitempty"`
	DateSubmitted   *string          `json:"dateSubmitted,omitempty"`
	DateModified    string           `json:"dateModified,omitempty"`
	Name            *string          `json:"name,omitempty"`
	Favorite        bool             `json:"favorite,omitempty"`
	Hidden          bool             `json:"hidden,omitempty"`
	Color           json.RawMessage  `json:"color,omitempty"`
	Category        json.RawMessage  `json:"category,omitempty"`
	Tags            []string         `json:"tags,omitempty"`
	Classifications []Classification `json:"classifications,omitempty"`
	Grade           string           `json:"grade,omitempty"`
	Stage           string           `json:"stage,omitempty"`
	Status          string           `json:"status,omitempty"`
	Is              *IsBlock         `json:"is,omitempty"`
	Os              json.RawMessage  `json:"os,omitempty"`
	Train           json.RawMessage  `json:"train,omitempty"`
	Test            json.RawMessage  `json:"test,omitempty"`
	Prod            json.RawMessage  `json:"prod,omitempty"`
	Competitions    json.RawMessage  `json:"competitions,omitempty"`
	Themes          json.RawMessage  `json:"themes,omitempty"`
	Pyramids        json.RawMessage  `json:"pyramids,omitempty"`
	PyramidThemes   json.RawMessage  `json:"pyramidThemes,omitempty"`
	Team            json.RawMessage  `json:"team,omitempty"`
	Origin          json.RawMessage  `json:"origin,omitempty"`
}

// Classification is one entry of Alpha.Classifications.
type Classification struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// IsBlock is the IS-stage metrics block on an Alpha.
type IsBlock struct {
	PnL             json.Number     `json:"pnl,omitempty"`
	BookSize        json.Number     `json:"bookSize,omitempty"`
	LongCount       int             `json:"longCount,omitempty"`
	ShortCount      int             `json:"shortCount,omitempty"`
	Turnover        float64         `json:"turnover,omitempty"`
	Returns         float64         `json:"returns,omitempty"`
	Drawdown        float64         `json:"drawdown,omitempty"`
	Margin          float64         `json:"margin,omitempty"`
	Sharpe          float64         `json:"sharpe,omitempty"`
	Fitness         float64         `json:"fitness,omitempty"`
	StartDate       string          `json:"startDate,omitempty"`
	SelfCorrelation *float64        `json:"selfCorrelation,omitempty"` // ACTIVE alphas only
	ProdCorrelation *float64        `json:"prodCorrelation,omitempty"` // ACTIVE alphas only
	Checks          []Check         `json:"checks,omitempty"`
	SelfCorrelated  *RecordSetBlock `json:"selfCorrelated,omitempty"`
}

// Check is one item of IsBlock.Checks.
type Check struct {
	Name   string   `json:"name"`
	Result string   `json:"result"` // PASS | WARNING | FAIL | PENDING | ERROR
	Limit  *float64 `json:"limit,omitempty"`
	Value  *float64 `json:"value,omitempty"`
	// Message is a human-readable note carried by non-numeric checks — set on
	// WARNING / informational results (e.g. REVERSION_COMPONENT) that have no
	// limit/value pair. Empty for the usual threshold checks.
	Message      string          `json:"message,omitempty"`
	Competitions json.RawMessage `json:"competitions,omitempty"`
}

// UnmarshalJSON tolerates BRAIN returning a check's limit/value as EITHER a JSON
// number OR a string. The threshold checks (LOW_SHARPE, HIGH_TURNOVER, …) carry
// numbers, but a few CATEGORICAL checks carry strings — verified live 2026-07-02,
// HT_ORTHOGONAL_RAM_NEUTRALIZATION reports {"limit":"RAM","value":"Subindustry"}.
// A number (or a numeric string) is parsed into the *float64; a non-numeric
// string (a category label, not a threshold) leaves the pointer nil.
//
// Without this, a single string-typed limit/value fails the decode of the WHOLE
// Alpha — encoding/json cannot put a string into *float64 — which breaks GET
// /alphas/{id}, the alphas list, AND the set-properties response (all decode an
// Alpha carrying is.checks). See parseCheckFloat.
func (c *Check) UnmarshalJSON(b []byte) error {
	// checkAlias drops the UnmarshalJSON method so the embedded decode does not
	// recurse; the shallower RawMessage limit/value shadow the alias's *float64
	// (Go's json prefers the least-nested field for a given tag), so we capture
	// the raw scalars here and coerce them below.
	type checkAlias Check
	shadow := struct {
		*checkAlias
		Limit json.RawMessage `json:"limit"`
		Value json.RawMessage `json:"value"`
	}{checkAlias: (*checkAlias)(c)}
	if err := json.Unmarshal(b, &shadow); err != nil {
		return err
	}
	c.Limit = parseCheckFloat(shadow.Limit)
	c.Value = parseCheckFloat(shadow.Value)
	return nil
}

// parseCheckFloat coerces a raw check limit/value scalar into a *float64: a JSON
// number or a numeric string yields the parsed value; null / absent / a
// non-numeric string yields nil (a categorical label carries no threshold).
func parseCheckFloat(raw json.RawMessage) *float64 {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return &f
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if pf, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
			return &pf
		}
	}
	return nil
}

// RecordSetBlock is the {schema, records} envelope used by /recordsets/pnl,
// /activities/* and selfCorrelated. records is an array of heterogeneous-typed
// tuples — keep raw for typed access via Schema.Properties[*].Name.
type RecordSetBlock struct {
	Schema  *RecordSchema     `json:"schema,omitempty"`
	Records []json.RawMessage `json:"records,omitempty"`
}

// SelfCorrelationBlock is the body returned by GET /alphas/{id}/correlations/self.
// Records are positional tuples per Schema.Properties[*].Name: id, name,
// instrumentType, region, universe, correlation, sharpe, returns, turnover,
// fitness, margin. Min/Max are server-computed aggregates across the records.
//
// Gate SubmitAlpha on *Max < 0.7: BRAIN rejects on the SELF_CORRELATION check
// at correlation >= 0.7, and that verdict only appears after a daily submit
// slot has been burned.
type SelfCorrelationBlock struct {
	RecordSetBlock
	Min *float64 `json:"min,omitempty"`
	Max *float64 `json:"max,omitempty"`
}

// PowerPoolCorrelationBlock is the body returned by
// GET /alphas/{id}/correlations/power-pool. Live probe (2026-07-02): the shape
// is IDENTICAL to /correlations/self — schema.name = "selfCorrelation", records
// are per-alpha positional tuples per Schema.Properties[*].Name (id, name,
// instrumentType, region, universe, correlation, sharpe, returns, turnover,
// fitness, margin), NOT the prod-corr histogram. Min/Max are the server-computed
// signed correlation extremes across the Power Pool members.
//
// FAIL-OPEN on an EMPTY pool: a fresh Power-Pool account has no comparable PP
// alpha, so the live probe returned records=[] with max=null (Min/Max nil).
// Consumers MUST treat a nil Max as "no constraint / pool empty" (pass), never
// as a zero correlation — gating on `*Max` without the nil guard would panic,
// and treating nil as 0 would spuriously pass a real gate. Gate on
// *Max < powerpool_corr_target (0.5) ONLY when Max is non-nil.
type PowerPoolCorrelationBlock struct {
	RecordSetBlock
	Min *float64 `json:"min,omitempty"`
	Max *float64 `json:"max,omitempty"`
}

// ProdCorrelationBlock is the body returned by GET /alphas/{id}/correlations/prod.
// Unlike /correlations/self (whose records are per-alpha tuples), the prod-corr
// records form a HISTOGRAM: each record is a positional tuple [binMin, binMax,
// count] per Schema.Properties[*].Name (min, max, alphas) — the number of BRAIN
// PRODUCTION alphas whose correlation to this alpha falls in [binMin, binMax).
// Min/Max are the server-computed signed correlation extremes across the whole
// production pool.
//
// Gate on *Max: BRAIN rejects on the SELF_CORRELATION-style prod check at
// correlation >= 0.7 (ACE's check_prod_corr_test uses the same top-level max).
// A trivial rank(close) alpha was observed with self-corr max 0.52 (passes) but
// prod-corr max 0.88 (fails) — the prod gate catches twins the self gate cannot.
type ProdCorrelationBlock struct {
	RecordSetBlock
	Min *float64 `json:"min,omitempty"`
	Max *float64 `json:"max,omitempty"`
}

// PerformanceStats is one side (before or after) of a BeforeAndAfterPerformance
// comparison: the aggregate metrics the alpha would carry.
type PerformanceStats struct {
	BookSize   float64 `json:"bookSize"`
	PnL        float64 `json:"pnl"`
	LongCount  int     `json:"longCount"`
	ShortCount int     `json:"shortCount"`
	Drawdown   float64 `json:"drawdown"`
	Turnover   float64 `json:"turnover"`
	Returns    float64 `json:"returns"`
	Margin     float64 `json:"margin"`
	Sharpe     float64 `json:"sharpe"`
	Fitness    float64 `json:"fitness"`
}

// BeforeAndAfterPerformance is the body of
// GET /competitions/{cid}/alphas/{aid}/before-and-after-performance — the
// projected impact of submitting an unsubmitted alpha into a competition (the
// "Performance Comparison" panel on the unsubmitted-alpha page).
//
// Score is the competition score (e.g. Delay-1) before vs after submission.
// Stats holds the aggregate metrics per side. YearlyStats.{Before,After} and PnL
// are positional RecordSetBlocks — decode via Schema.Properties[*].Name; the PnL
// columns are date, beforePnL, afterPnL. Competition and Team are kept raw, same
// as Alpha.Competitions / Alpha.Team.
type BeforeAndAfterPerformance struct {
	PartitionName string `json:"partitionName"`
	Score         struct {
		Before float64 `json:"before"`
		After  float64 `json:"after"`
	} `json:"score"`
	Stats struct {
		Before PerformanceStats `json:"before"`
		After  PerformanceStats `json:"after"`
	} `json:"stats"`
	YearlyStats struct {
		Before RecordSetBlock `json:"before"`
		After  RecordSetBlock `json:"after"`
	} `json:"yearlyStats"`
	PnL         RecordSetBlock  `json:"pnl"`
	Competition json.RawMessage `json:"competition,omitempty"`
	Team        json.RawMessage `json:"team,omitempty"`
	Partition   []string        `json:"partition,omitempty"`
}

// RecordSchema describes the columnar shape of RecordSetBlock.Records.
type RecordSchema struct {
	Name       string           `json:"name,omitempty"`
	Title      string           `json:"title,omitempty"`
	Properties []SchemaProperty `json:"properties,omitempty"`
}

// SchemaProperty is one column descriptor.
type SchemaProperty struct {
	Name  string `json:"name"`
	Title string `json:"title,omitempty"`
	Type  string `json:"type"`
}

// Verdict is the parsed terminal outcome of POST + GET long-poll on
// /alphas/{id}/submit. Status is one of:
//
//	verified       — alpha was activated
//	corr_rejected  — SELF_CORRELATION FAIL
//	submit_failed  — any other non-CORR FAIL
//	pending_corr   — long-poll cap exceeded, verdict still computing
type Verdict struct {
	Status string  `json:"status"`
	Reason string  `json:"reason,omitempty"`
	Body   *Alpha  `json:"body,omitempty"`
	Checks []Check `json:"checks,omitempty"`
	HTTP   int     `json:"http,omitempty"`
}

// Simulation is the body of GET /simulations/{id}. It doubles as a
// multi-simulation PARENT (Children populated, no Alpha) and CHILD (Parent
// populated, produces an Alpha).
type Simulation struct {
	ID       string          `json:"id,omitempty"`
	Parent   string          `json:"parent,omitempty"`   // set when this sim is a multi-sim child
	Children []string        `json:"children,omitempty"` // set when this sim is a multi-sim parent
	Type     string          `json:"type,omitempty"`
	Settings json.RawMessage `json:"settings,omitempty"`
	Regular  json.RawMessage `json:"regular,omitempty"`
	Status   string          `json:"status,omitempty"`   // WAITING|SIMULATING (running); COMPLETE|WARNING|CANCELLED|TIMEOUT|ERROR|FAIL (terminal)
	Alpha    string          `json:"alpha,omitempty"`    // populated on COMPLETE/WARNING (single or child)
	Message  string          `json:"message,omitempty"`  // populated on FAIL/ERROR
	Progress *float64        `json:"progress,omitempty"` // [0..1] while running
}

// RateLimit is the daily simulation-quota snapshot parsed from the
// X-Ratelimit-* headers of a POST /simulations response. Present is false when
// the server did not send them. (BRAIN's CORS Access-Control-Expose-Headers
// omits X-Ratelimit-*, so a browser fetch reads null — only the SDK/CLI can
// see them.) Reset is the time until the quota resets (EST challenge-day).
type RateLimit struct {
	Limit     int           `json:"limit"`
	Remaining int           `json:"remaining"`
	Reset     time.Duration `json:"reset"`
	Present   bool          `json:"present"`
}

// CreateSimulationResult is returned by CreateSimulation and
// CreateMultiSimulation. ID is the simulation id from the Location header (the
// PARENT id for a multi-simulation); RateLimit is the daily-quota snapshot from
// the same response.
type CreateSimulationResult struct {
	ID        string    `json:"id"`
	RateLimit RateLimit `json:"rateLimit"`
}

// SimulationRequest is the POST /simulations body.
type SimulationRequest struct {
	Type    string `json:"type"` // "REGULAR" | "SUPER" | "COMBO"
	Regular string `json:"regular,omitempty"`
	Super   string `json:"super,omitempty"`
	// Selection is the SUPER-alpha selection expression (the second leg of a
	// SUPER alpha alongside Super). omitempty so REGULAR/COMBO round-trip clean.
	Selection string          `json:"selection,omitempty"`
	Settings  SimSettings     `json:"settings"`
	Combo     json.RawMessage `json:"combo,omitempty"`
}

// SimSettings captures the simulate-time knobs. Fields match BRAIN's exact
// names (camelCase) so a SimulationRequest round-trips byte-for-byte through
// the SDK.
type SimSettings struct {
	InstrumentType string  `json:"instrumentType"`
	Region         string  `json:"region"`
	Universe       string  `json:"universe"`
	Delay          int     `json:"delay"`
	Decay          int     `json:"decay"`
	Neutralization string  `json:"neutralization"`
	Truncation     float64 `json:"truncation"`
	Pasteurization string  `json:"pasteurization"`
	UnitHandling   string  `json:"unitHandling"`
	NanHandling    string  `json:"nanHandling"`
	Language       string  `json:"language"`
	Visualization  bool    `json:"visualization"`

	// The following are CONSULTANT-era knobs, all omitempty so an existing
	// SimulationRequest that omits them still round-trips byte-for-byte through
	// the SDK. Enum values below are from the live OPTIONS /simulations schema
	// (2026-07-01); the SDK does NOT hard-validate them — the server is the
	// authority — but the exact accepted values are documented here.

	// TestPeriod is an ISO-8601 duration string (e.g. "P2Y", "P6M") selecting
	// the out-of-sample test window length. Empty = BRAIN default.
	TestPeriod string `json:"testPeriod,omitempty"`
	// MaxTrade ∈ {"OFF","ON"}: cap per-name trade size. Some regions
	// (ASI/JPN/HKG/KOR/TWN) require "ON".
	MaxTrade string `json:"maxTrade,omitempty"`
	// MaxPosition ∈ {"OFF","ON"}: cap per-name position size.
	MaxPosition string `json:"maxPosition,omitempty"`
	// SelectionHandling ∈ {"POSITIVE","NON_ZERO","NON_NAN"}: how the SUPER
	// alpha selection expression's output is interpreted.
	SelectionHandling string `json:"selectionHandling,omitempty"`
	// SelectionLimit is the max number of instruments the SUPER selection keeps.
	SelectionLimit int `json:"selectionLimit,omitempty"`
	// ComponentActivation ∈ {"IS","OS"}: for a SUPER alpha, when each component
	// alpha starts contributing to the combo — from its In-Sample start ("IS",
	// longer 10y history, overfit-prone) or its Out-of-Sample start ("OS",
	// overfit-resistant, the OS reality check). SuperAlpha-only; omitempty so
	// REGULAR/COMBO round-trip byte-for-byte. Enum + semantics per the BRAIN
	// superalpha-overview.md "Component Activation" section (confirmed a SUPER
	// setting, IS/OS the only two modes). Exact JSON key ("componentActivation")
	// is the doc's field name; the SDK does not hard-validate — the server is
	// authority.
	//
	// UNVERIFIED as a sim setting (live OPTIONS /simulations probe, 2026-07-02):
	// the schema does NOT list componentActivation in ANY casing / underscore
	// form (only selection/combo are there), and it is absent from a REGULAR
	// alpha's `settings` too. Its real home is still unconfirmed — it may be an
	// alpha-level PATCH attribute rather than a sim setting. Kept here because
	// omitempty means "not set => not sent", so an unverified field can ship
	// harmlessly until the FIRST SUPER simulation confirms where the server
	// reads it. Do NOT assume a SUPER sim will honor it until verified.
	ComponentActivation string `json:"componentActivation,omitempty"`
}

// Operator is one item from GET /operators (bare array).
type Operator struct {
	Name          string   `json:"name"`
	Category      string   `json:"category"`
	Scope         []string `json:"scope"`
	Definition    string   `json:"definition"`
	Description   string   `json:"description,omitempty"`
	Documentation string   `json:"documentation,omitempty"`
	Level         string   `json:"level,omitempty"`
}

// DataField is one item from GET /data-fields results[].
type DataField struct {
	ID           string     `json:"id"`
	Region       string     `json:"region"`
	Universe     string     `json:"universe"`
	Delay        int        `json:"delay"`
	Type         string     `json:"type"`
	Category     NamedRef   `json:"category"`
	Dataset      NamedRef   `json:"dataset"`
	Subcategory  NamedRef   `json:"subcategory"`
	Description  string     `json:"description"`
	Coverage     float64    `json:"coverage"`
	DateCoverage float64    `json:"dateCoverage"`
	AlphaCount   int        `json:"alphaCount"`
	UserCount    int        `json:"userCount"`
	Themes       []NamedRef `json:"themes"`
	// DateCreated is BRAIN's month-granularity "Date added" (e.g. "2026-03-01"),
	// added to GET /data-fields on 2026-06-11. Empty for responses that predate it.
	DateCreated string `json:"dateCreated"`
}

// NamedRef is the {id, name} shape used inside DataField.
type NamedRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Dataset is one item from GET /data-sets results[]. It mirrors the Data
// Explorer "Datasets" tab: a dataset groups many data-fields and carries the
// crowding / value signals a consultant steers on.
//
// ValueScore is the CONSULTANT-only "Dataset Value Score" — a measure of a
// dataset's UNDER-utilization; consultants are advised to research datasets
// with a HIGHER value score (glossary.md: "Dataset Value Score"). It is a
// pointer so an absent field (non-consultant tier, or a schema that predates
// it) decodes as nil rather than a misleading 0. PyramidMultiplier is the
// Dynamic Pyramid Theme multiplier shown on the Dataset page
// (themes/overview-themes.md) — also a pointer, present only when a pyramid
// theme is live for the dataset.
//
// The exact JSON key names for ValueScore / PyramidMultiplier are NOT pinned by
// any local reference doc (brain-api.md documents /data-fields but not
// /data-sets), so the struct decodes a few plausible aliases via a custom
// UnmarshalJSON and the SDK never hard-requires them (all pointer/omitempty).
// This keeps the endpoint fail-open pending an active probe against the live
// /data-sets response — see schema.go Datasets doc.
type Dataset struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Region      string   `json:"region,omitempty"`
	Universe    string   `json:"universe,omitempty"`
	Delay       int      `json:"delay,omitempty"`
	Category    NamedRef `json:"category,omitempty"`
	Subcategory NamedRef `json:"subcategory,omitempty"`
	Coverage    *float64 `json:"coverage,omitempty"`
	// FieldCount is the number of data-fields in the dataset (BRAIN sends it as
	// "fieldCount" or "valueFieldCount" depending on payload; both decoded).
	FieldCount *int `json:"fieldCount,omitempty"`
	AlphaCount *int `json:"alphaCount,omitempty"`
	UserCount  *int `json:"userCount,omitempty"`
	// ValueScore is the Dataset Value Score (consultant-only, higher = more
	// under-utilized = more valuable to research). Pointer: nil when absent.
	ValueScore *float64 `json:"valueScore,omitempty"`
	// PyramidMultiplier is the Dynamic Pyramid Theme multiplier (nil when the
	// dataset has no live pyramid theme).
	PyramidMultiplier *float64   `json:"pyramidMultiplier,omitempty"`
	Themes            []NamedRef `json:"themes,omitempty"`
}

// UnmarshalJSON decodes a Dataset from the live /data-sets payload, tolerating
// the field-name uncertainty around the consultant-only signals. It first
// decodes the stable keys via an alias, then rescues ValueScore /
// PyramidMultiplier / FieldCount from any of their plausible aliases so a schema
// drift (or an active-probe-confirmed rename) does not silently drop the signal.
func (d *Dataset) UnmarshalJSON(b []byte) error {
	type alias Dataset
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*d = Dataset(a)
	// Rescue the loosely-named / uncertain keys from a generic map.
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		// Deliberate fail-open (nilerr): the alias decode above already
		// succeeded, so the stable fields are populated — the rescue pass is
		// best-effort for uncertain consultant-only keys and must not turn a
		// decodable payload into an error.
		return nil //nolint:nilerr
	}
	if d.ValueScore == nil {
		d.ValueScore = firstFloatPtr(m, "valueScore", "value_score", "datasetValueScore")
	}
	if d.PyramidMultiplier == nil {
		d.PyramidMultiplier = firstFloatPtr(m, "pyramidMultiplier", "pyramid_multiplier", "multiplier")
	}
	if d.FieldCount == nil {
		d.FieldCount = firstIntPtr(m, "fieldCount", "valueFieldCount", "fieldsCount")
	}
	return nil
}

// firstFloatPtr returns a pointer to the first key in keys that decodes to a
// float, or nil.
func firstFloatPtr(m map[string]json.RawMessage, keys ...string) *float64 {
	for _, k := range keys {
		if raw, ok := m[k]; ok {
			var f float64
			if json.Unmarshal(raw, &f) == nil {
				return &f
			}
		}
	}
	return nil
}

// firstIntPtr returns a pointer to the first key in keys that decodes to an int,
// or nil.
func firstIntPtr(m map[string]json.RawMessage, keys ...string) *int {
	for _, k := range keys {
		if raw, ok := m[k]; ok {
			var i int
			if json.Unmarshal(raw, &i) == nil {
				return &i
			}
		}
	}
	return nil
}

// DatasetsPage is the GET /data-sets envelope ({count, results}) mirroring
// DataFieldsPage.
type DatasetsPage struct {
	Count   int       `json:"count"`
	Results []Dataset `json:"results"`
}

// DataCategory is one item from GET /data-categories. Live probe (2026-07-02):
// the endpoint returns a BARE JSON array (no {count, results} envelope) of
// category descriptors. Each carries a category-level ValueScore (float — the
// under-utilization signal, aggregated over the category's datasets), the list
// of Regions the category covers, crowding/size counts, and a Children array of
// subcategories with the same shape. All the numeric signals are pointers so an
// absent field decodes as nil rather than a misleading 0.
type DataCategory struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	// ValueScore is the category-level Dataset Value Score (float, higher = more
	// under-utilized). Pointer: nil when absent.
	ValueScore *float64 `json:"valueScore,omitempty"`
	// Region is the list of regions the category has data for (probe: an array).
	Region       []string       `json:"region,omitempty"`
	DatasetCount *int           `json:"datasetCount,omitempty"`
	FieldCount   *int           `json:"fieldCount,omitempty"`
	AlphaCount   *int           `json:"alphaCount,omitempty"`
	UserCount    *int           `json:"userCount,omitempty"`
	Children     []DataCategory `json:"children,omitempty"`
}

// Theme is one BRAIN consultant Theme (a region/dataset/delay bonus running
// 1-3 weeks). A submitted alpha that satisfies a theme earns a QualityFactor
// multiplier; when several themes overlap the final multiplier is
// sum(multipliers) - count(themes) + 1 (themes/multiplier-rules.md).
//
// PROBED 404 (2026-07-02): there is no /themes endpoint (see Client.Themes) —
// this type is now vestigial. The API-authoritative "multiplier in effect"
// signal is Dataset.PyramidMultiplier off /data-sets; the human-readable theme
// calendar is the Learn page learn/documentation/themes/consgrpdefault. The
// Raw fallback + pointer Multiplier are retained so a future real endpoint
// decodes without a code change.
type Theme struct {
	ID         string          `json:"id,omitempty"`
	Name       string          `json:"name,omitempty"`
	Multiplier *float64        `json:"multiplier,omitempty"`
	Region     string          `json:"region,omitempty"`
	Delay      *int            `json:"delay,omitempty"`
	StartDate  string          `json:"startDate,omitempty"`
	EndDate    string          `json:"endDate,omitempty"`
	Datasets   []NamedRef      `json:"datasets,omitempty"`
	Raw        json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes a Theme, rescuing the multiplier from its plausible
// aliases and preserving the full object in Raw.
func (t *Theme) UnmarshalJSON(b []byte) error {
	type alias Theme
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*t = Theme(a)
	t.Raw = append(t.Raw[:0], b...)
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err == nil && t.Multiplier == nil {
		t.Multiplier = firstFloatPtr(m, "multiplier", "qualityFactorMultiplier", "qfMultiplier", "value")
	}
	return nil
}

// Competition is one item of GET /users/self/competitions results[].
type Competition struct {
	ID                string          `json:"id"`
	Name              string          `json:"name"`
	Description       string          `json:"description"`
	Scoring           string          `json:"scoring"`
	Status            string          `json:"status"`
	SignUpDate        *string         `json:"signUpDate"`
	StartDate         *string         `json:"startDate"`
	EndDate           *string         `json:"endDate"`
	SignUpStartDate   *string         `json:"signUpStartDate"`
	SignUpEndDate     *string         `json:"signUpEndDate"`
	Leaderboard       *Leaderboard    `json:"leaderboard"`
	Countries         json.RawMessage `json:"countries"`
	ExcludedCountries json.RawMessage `json:"excludedCountries"`
	Universities      json.RawMessage `json:"universities"`
	Team              json.RawMessage `json:"team"`
	TeamBased         bool            `json:"teamBased"`
	PrizeBoard        bool            `json:"prizeBoard"`
	UniversityBoard   bool            `json:"universityBoard"`
	Submissions       bool            `json:"submissions"`
	Progress          json.RawMessage `json:"progress"`
	FAQ               string          `json:"faq"`
}

// Leaderboard is the user's standing inside a Competition.
//
// Score is intentionally json.Number — the BRAIN edge returns it as an int
// for active users with non-zero standings (e.g. 45565) but as a float "0.0"
// for accounts with no competition activity. Captured live 2026-05-18.
type Leaderboard struct {
	Alphas     int             `json:"alphas"`
	Country    string          `json:"country"`
	Level      string          `json:"level"`
	Rank       int             `json:"rank"`
	Score      json.Number     `json:"score"`
	University json.RawMessage `json:"university"`
	User       string          `json:"user"`
}

// ActivityKind is the path-segment for GET /users/self/activities/{kind}.
type ActivityKind string

const (
	ActivityBasePayment  ActivityKind = "base-payment"
	ActivityOtherPayment ActivityKind = "other-payment"
	ActivitySimulations  ActivityKind = "simulations"
	ActivitySubmissions  ActivityKind = "submissions"
)

// ActivityType discriminates the two envelope shapes BRAIN returns for
// /activities/*: "DAILY" includes the yesterday/current/previous/ytd buckets;
// "LIST" only has total.
type ActivityType string

const (
	ActivityTypeDaily ActivityType = "DAILY"
	ActivityTypeList  ActivityType = "LIST"
)

// ActivityPeriod is one of the named summary windows.
type ActivityPeriod struct {
	Start string      `json:"start"`
	End   string      `json:"end"`
	Value json.Number `json:"value"`
}

// ActivityStream is the body of GET /users/self/activities/{kind}.
type ActivityStream struct {
	Type      ActivityType    `json:"type"`
	Currency  string          `json:"currency,omitempty"`
	Yesterday *ActivityPeriod `json:"yesterday,omitempty"`
	Current   *ActivityPeriod `json:"current,omitempty"`
	Previous  *ActivityPeriod `json:"previous,omitempty"`
	YTD       *ActivityPeriod `json:"ytd,omitempty"`
	Total     *ActivityPeriod `json:"total,omitempty"`
	Records   *RecordSetBlock `json:"records,omitempty"`
}

// DiversityStream is the body of GET /users/self/activities/diversity. It is a
// DISTINCT shape from ActivityStream (the payment/simulation/submission
// activities envelope) — the diversity endpoint reports the spread of the
// user's alphas across a grouping dimension (dataset, region, universe, …).
//
// No live shape has been captured yet, so the body is carried as raw JSON and
// passed through untyped. When a real shape is confirmed, promote the common
// keys to typed fields (mirroring ActivityStream) and keep a Raw fallback.
type DiversityStream struct {
	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON stores the whole body verbatim (pass-through until the shape is
// characterized).
func (d *DiversityStream) UnmarshalJSON(b []byte) error {
	d.Raw = append(d.Raw[:0], b...)
	return nil
}

// MarshalJSON re-emits the stored raw body so `users diversity` prints exactly
// what BRAIN returned.
func (d DiversityStream) MarshalJSON() ([]byte, error) {
	if len(d.Raw) == 0 {
		return []byte("null"), nil
	}
	return d.Raw, nil
}

// PnLSeries is the body of GET /alphas/{id}/recordsets/pnl.
type PnLSeries struct {
	Schema  *RecordSchema `json:"schema"`
	Records []PnLPoint    `json:"records"`
}

// PnLPoint is one [date, value] tuple from PnLSeries.Records.
type PnLPoint struct {
	Date  string
	Value float64
}

// UnmarshalJSON decodes the heterogeneous-typed [string, number] tuple.
func (p *PnLPoint) UnmarshalJSON(b []byte) error {
	var raw [2]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if err := json.Unmarshal(raw[0], &p.Date); err != nil {
		return err
	}
	// BRAIN emits null (or a 1-element tuple) for the value on non-trading / gap
	// days. The return/corr math (TrimPnLToYears, PnLToReturns) already drops
	// non-finite points, so decode a null/absent value as NaN rather than failing
	// the ENTIRE series on a single gap. MarshalJSON re-encodes NaN back to null.
	if len(raw[1]) == 0 || string(raw[1]) == "null" {
		p.Value = math.NaN()
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(raw[1], &n); err != nil {
		return err
	}
	v, err := n.Float64()
	if err != nil {
		return err
	}
	p.Value = v
	return nil
}

// MarshalJSON re-encodes a PnLPoint as the [date, value] tuple BRAIN emits.
func (p PnLPoint) MarshalJSON() ([]byte, error) {
	// A non-finite value is a decoded gap (see UnmarshalJSON); re-encode it as
	// null — both faithful to BRAIN's wire shape and valid JSON (json.Marshal
	// rejects NaN/Inf, which would otherwise break the `alphas pnl` output).
	if !isFinite(p.Value) {
		return json.Marshal([2]any{p.Date, nil})
	}
	return json.Marshal([2]any{p.Date, p.Value})
}

// Page is a typed wrapper for the Django REST envelope used by
// /users/self/alphas and /users/self/competitions.
type Page[T any] struct {
	Count    int     `json:"count"`
	Next     *string `json:"next"`
	Previous *string `json:"previous"`
	Results  []T     `json:"results"`
}

// DataFieldsPage is the (no-next, no-previous) flavor used by /data-fields.
type DataFieldsPage struct {
	Count   int         `json:"count"`
	Results []DataField `json:"results"`
}

// ListAlphasOptions configures GET /users/self/alphas.
type ListAlphasOptions struct {
	Status string // "ACTIVE" | "UNSUBMITTED" | "DECOMMISSIONED" | ""
	Limit  int    // default 100 per BRAIN
	Offset int
	Order  string // "-dateCreated" etc.
	// Filters are BRAIN comparison filters in logical form, e.g.
	// "is.sharpe>=1.25", "is.fitness>=1", "is.turnover<=0.7". The operator
	// (>, >=, <, <=) is embedded in the field token; multiple filters AND together.
	// Each is percent-encoded and appended raw (BRAIN rejects DRF "__gte" with 400).
	// Verified against the live endpoint 2026-06-07.
	Filters []string
}

// Message is one item of GET /users/self/messages results[] — the feed behind
// the BRAIN platform notification center
// (platform.worldquantbrain.com/messages/notifications). Field set
// live-confirmed 2026-05-27.
//
// The web UI splits the feed into two tabs that map 1:1 to Type: "Announcements"
// (Type=="ANNOUNCEMENT") and "Notifications" (Type=="NOTIFICATION"). Dataset
// releases — the high-value "new dataset" notices — arrive as ANNOUNCEMENT
// messages identified by Title (e.g. "📢 Launching a new dataset …"); there is
// no dedicated type or tag for them, so callers filter on Title client-side.
//
// Description is rendered HTML. It is short in practice (≤ ~2 KB observed) but
// MAY embed base64 data-URI <img> tags that run to several MB; callers piping
// it into size-sensitive sinks (LLM prompts, logs) should strip or summarize
// it. The SDK transports it verbatim.
type Message struct {
	ID          string   `json:"id"`
	Type        string   `json:"type"` // "ANNOUNCEMENT" | "NOTIFICATION" (full set, live-confirmed)
	Title       string   `json:"title"`
	Description string   `json:"description"`
	DateCreated string   `json:"dateCreated"`
	Tags        []string `json:"tags"`
	Read        bool     `json:"read"`
}

// ListMessagesOptions configures GET /users/self/messages.
type ListMessagesOptions struct {
	Type   string // "ANNOUNCEMENT" | "NOTIFICATION"; empty = all types (server accepts no filter)
	Limit  int    // BRAIN's web client uses 10
	Offset int
	Order  string // "-dateCreated" etc.
}

// DataFieldsQuery configures GET /data-fields. All four core fields are
// REQUIRED — BRAIN returns 400 if any is missing.
type DataFieldsQuery struct {
	InstrumentType string // "EQUITY"
	Region         string // "USA"
	Universe       string // "TOP3000"
	Delay          int    // 0 or 1
	Limit          int
	Offset         int
	// Dataset narrows /data-fields to one dataset (query param `dataset.id=`).
	// BRAIN clamps `count` at 10,000 and 400s any offset at/past it ("Invalid
	// offset. Please use filters to narrow down the result."), so a slice whose
	// field universe exceeds 10k MUST be drained per-dataset. Empty = no filter.
	Dataset string
}

// RegisterInput is the high-level POST /users payload. The SDK fills in
// Auxiliary.Captcha automatically via the configured CaptchaSolver.
type RegisterInput struct {
	Email      string          `json:"email"`
	FirstName  string          `json:"firstName"`
	LastName   string          `json:"lastName"`
	FullName   string          `json:"fullName"`
	Gender     string          `json:"gender"`
	Telephone  string          `json:"telephone,omitempty"`
	Address    Address         `json:"address"`
	Education  Education       `json:"education"`
	Employment json.RawMessage `json:"employment,omitempty"`
	Auxiliary  Auxiliary       `json:"auxiliary"`
}

// Address is a sub-object of RegisterInput.
type Address struct {
	Country string `json:"country"`
	State   string `json:"state,omitempty"`
	City    string `json:"city,omitempty"`
	Street  string `json:"street,omitempty"`
}

// Education is a sub-object of RegisterInput. Degree must be one of
// BACHELORS / MASTERS / ASSOCIATE — BRAIN rejects other values with
// `"\"X\" is not a valid choice."` (live-confirmed 2026-05-19).
type Education struct {
	University     string `json:"university"`
	Major          string `json:"major"`
	Degree         string `json:"degree"`
	GraduationYear int    `json:"graduationYear"`
}

// Auxiliary is the sub-object of RegisterInput. Captcha is auto-populated
// by the SDK after a successful CaptchaSolver round.
type Auxiliary struct {
	Agree        []string `json:"agree"`
	Password     string   `json:"password"`
	Confirmation string   `json:"confirmation"`
	Captcha      string   `json:"captcha,omitempty"`
}

// CookieSnapshot is used by the file-backed jar to persist cookies to disk.
type CookieSnapshot struct {
	URL     string        `json:"url"`
	Cookies []CookieEntry `json:"cookies"`
	Saved   time.Time     `json:"saved"`
}

// CookieEntry is one persisted cookie.
type CookieEntry struct {
	Name    string    `json:"name"`
	Value   string    `json:"value"`
	Path    string    `json:"path,omitempty"`
	Domain  string    `json:"domain,omitempty"`
	Expires time.Time `json:"expires,omitempty"`
	Secure  bool      `json:"secure,omitempty"`
	HTTP    bool      `json:"httpOnly,omitempty"`
}
