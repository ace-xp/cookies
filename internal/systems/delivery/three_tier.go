package delivery

import "time"

const ThreeTierSchema = "delivery-three-tier/v1"

type ThreeTierFixture = Scenario

// ThreeTierConfiguration is a complete, mock-only delivery snapshot. It is
// deliberately attached to a PlanVersion rather than a mutable plan row.
type ThreeTierConfiguration struct {
	Schema          string           `json:"schema"`
	Source          Source           `json:"source"`
	Scenario        ThreeTierFixture `json:"scenario"`
	FixtureScenario ThreeTierFixture `json:"fixture_scenario"`
	GeneratedAt     time.Time        `json:"generated_at"`
	Evidence        []string         `json:"evidence"`
	Groups          []ThreeTierGroup `json:"groups"`
}
type ThreeTierGroup struct {
	ID     string           `json:"id"`
	Name   string           `json:"name"`
	Fields []ThreeTierField `json:"fields"`
	Plans  []ThreeTierPlan  `json:"plans"`
}
type ThreeTierPlan struct {
	ID        string              `json:"id"`
	Name      string              `json:"name"`
	Fields    []ThreeTierField    `json:"fields"`
	Creatives []ThreeTierCreative `json:"creatives"`
}
type ThreeTierCreative struct {
	ID     string           `json:"id"`
	Name   string           `json:"name"`
	Fields []ThreeTierField `json:"fields"`
}
type ThreeTierValue struct {
	Type  string `json:"type"`
	Value any    `json:"value"`
}
type ThreeTierField struct {
	Key              string          `json:"key"`
	Label            string          `json:"label"`
	Recommended      ThreeTierValue  `json:"recommended"`
	Manual           *ThreeTierValue `json:"manual,omitempty"`
	Effective        ThreeTierValue  `json:"effective"`
	Source           string          `json:"source"`
	SourceRefs       []string        `json:"source_refs"`
	EffectiveSource  string          `json:"effective_source"`
	Dependency       string          `json:"dependency,omitempty"`
	DependencyRefs   []string        `json:"dependency_refs"`
	Risk             string          `json:"risk,omitempty"`
	RiskRefs         []string        `json:"risk_refs"`
	EvidenceRefs     []string        `json:"evidence_refs"`
	MockRequired     bool            `json:"mock_required"`
	PlatformRequired bool            `json:"platform_required"`
	PlatformStatus   string          `json:"platform_status"`
	Editable         bool            `json:"editable"`
	Confirmed        bool            `json:"confirmation"`
}

func cloneThreeTierConfiguration(v *ThreeTierConfiguration) *ThreeTierConfiguration {
	if v == nil {
		return nil
	}
	b := *v
	b.Evidence = append([]string(nil), v.Evidence...)
	b.Groups = append([]ThreeTierGroup(nil), v.Groups...)
	for i := range b.Groups {
		b.Groups[i].Fields = cloneThreeTierFields(v.Groups[i].Fields)
		b.Groups[i].Plans = append([]ThreeTierPlan(nil), v.Groups[i].Plans...)
		for j := range b.Groups[i].Plans {
			b.Groups[i].Plans[j].Fields = cloneThreeTierFields(v.Groups[i].Plans[j].Fields)
			b.Groups[i].Plans[j].Creatives = append([]ThreeTierCreative(nil), v.Groups[i].Plans[j].Creatives...)
			for k := range b.Groups[i].Plans[j].Creatives {
				b.Groups[i].Plans[j].Creatives[k].Fields = cloneThreeTierFields(v.Groups[i].Plans[j].Creatives[k].Fields)
			}
		}
	}
	return &b
}
func cloneThreeTierFields(values []ThreeTierField) []ThreeTierField {
	out := append([]ThreeTierField(nil), values...)
	for i := range out {
		f := &out[i]
		f.EvidenceRefs = append([]string(nil), f.EvidenceRefs...)
		f.SourceRefs = append([]string(nil), f.SourceRefs...)
		f.DependencyRefs = append([]string(nil), f.DependencyRefs...)
		f.RiskRefs = append([]string(nil), f.RiskRefs...)
		if f.Manual != nil {
			m := *f.Manual
			f.Manual = &m
		}
	}
	return out
}
