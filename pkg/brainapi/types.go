package brainapi

import (
	"encoding/json"
	"math"
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

// Simulation is the body of GET /simulations/{id}.
type Simulation struct {
	ID       string          `json:"id,omitempty"`
	Type     string          `json:"type,omitempty"`
	Settings json.RawMessage `json:"settings,omitempty"`
	Regular  json.RawMessage `json:"regular,omitempty"`
	Status   string          `json:"status,omitempty"`   // COMPLETE | FAIL | ERROR | "" (still running)
	Alpha    string          `json:"alpha,omitempty"`    // populated on COMPLETE
	Message  string          `json:"message,omitempty"`  // populated on FAIL/ERROR
	Progress *float64        `json:"progress,omitempty"` // [0..1] while running
}

// SimulationRequest is the POST /simulations body.
type SimulationRequest struct {
	Type     string          `json:"type"` // "REGULAR" | "SUPER" | "COMBO"
	Regular  string          `json:"regular,omitempty"`
	Super    string          `json:"super,omitempty"`
	Settings SimSettings     `json:"settings"`
	Combo    json.RawMessage `json:"combo,omitempty"`
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
}

// NamedRef is the {id, name} shape used inside DataField.
type NamedRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
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
