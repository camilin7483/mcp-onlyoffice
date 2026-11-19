package operations

import (
	"reflect"
	"strings"
	"testing"
)

func TestPolicyMatrix(t *testing.T) {
	cases := []struct {
		risk                         RiskLevel
		approved                     bool
		wantAllow, wantAppr, wantDif bool
	}{
		{RiskReadOnly, false, true, false, false},
		{RiskReadOnly, true, true, false, false},
		{RiskSafeMutation, false, true, false, false},
		{RiskSafeMutation, true, true, false, false},
		{RiskSensitiveMutation, false, true, true, true},
		{RiskSensitiveMutation, true, true, false, true},
		{RiskDestructive, false, true, true, true},
		{RiskDestructive, true, true, true, true}, // approved still gated
		{RiskNetwork, false, false, false, false},
		{RiskNetwork, true, false, false, false},
		{RiskAdmin, false, false, false, false},
		{RiskAdmin, true, false, false, false},
	}
	for _, c := range cases {
		p := &Policy{}
		if c.approved {
			p.Approved = map[string]bool{"op": true}
		}
		d := p.Decide(Spec{Name: "op", Backend: BackendOOXML, Risk: c.risk, Capability: "c"})
		if d.Allow != c.wantAllow || d.RequireApproval != c.wantAppr || d.RequireDiff != c.wantDif {
			t.Errorf("risk=%v approved=%v: got %+v", c.risk, c.approved, d)
		}
		if (c.risk == RiskNetwork || c.risk == RiskAdmin) && !strings.Contains(d.Reason, "disabled by default") {
			t.Errorf("risk=%v: reason must say disabled by default: %q", c.risk, d.Reason)
		}
		if d.Reason == "" {
			t.Errorf("risk=%v: empty reason", c.risk)
		}
	}
}

func TestPolicyNilReceiver(t *testing.T) {
	var p *Policy
	d := p.Decide(Spec{Name: "op", Backend: BackendOOXML, Risk: RiskSensitiveMutation, Capability: "c"})
	if !d.Allow || !d.RequireApproval || !d.RequireDiff {
		t.Fatalf("nil policy must deny-by-approval for sensitive: %+v", d)
	}
}

func TestPolicyUnknownRiskDenied(t *testing.T) {
	d := (&Policy{}).Decide(Spec{Name: "op", Backend: BackendOOXML, Risk: RiskLevel(99), Capability: "c"})
	if d.Allow {
		t.Fatalf("unknown risk must be denied: %+v", d)
	}
}

func TestRegistryRegisterLookupNames(t *testing.T) {
	r := NewRegistry()
	specs := []Spec{
		{Name: "writer.replace_range", Backend: BackendDesktop, Risk: RiskSensitiveMutation, Capability: "writer"},
		{Name: "reader.get_text", Backend: BackendOOXML, Risk: RiskReadOnly, Capability: "reader"},
		{Name: "doc.delete", Backend: BackendPython, Risk: RiskDestructive, Capability: "doc", Reversible: true},
	}
	for _, s := range specs {
		if err := r.Register(s); err != nil {
			t.Fatalf("register %q: %v", s.Name, err)
		}
	}
	if err := r.Register(specs[0]); err == nil {
		t.Fatal("duplicate register must fail")
	}
	got, ok := r.Lookup("reader.get_text")
	if !ok || got.Risk != RiskReadOnly {
		t.Fatalf("lookup failed: %+v %v", got, ok)
	}
	if _, ok := r.Lookup("missing"); ok {
		t.Fatal("lookup of missing name must fail")
	}
	want := []string{"doc.delete", "reader.get_text", "writer.replace_range"}
	if names := r.Names(); !reflect.DeepEqual(names, want) {
		t.Fatalf("names not sorted: %v", names)
	}
}

func TestRegistryRejectsEmptyFields(t *testing.T) {
	base := Spec{Name: "n", Backend: BackendOOXML, Risk: RiskReadOnly, Capability: "c"}
	bad := []Spec{
		{Name: "", Backend: BackendOOXML, Risk: RiskReadOnly, Capability: "c"},
		{Name: "n", Backend: "", Risk: RiskReadOnly, Capability: "c"},
		{Name: "n", Backend: "nope", Risk: RiskReadOnly, Capability: "c"},
		{Name: "n", Backend: BackendOOXML, Risk: RiskLevel(99), Capability: "c"},
		{Name: "n", Backend: BackendOOXML, Risk: RiskReadOnly, Capability: ""},
	}
	_ = base
	for i, s := range bad {
		if err := NewRegistry().Register(s); err == nil {
			t.Fatalf("case %d must fail: %+v", i, s)
		}
	}
}
