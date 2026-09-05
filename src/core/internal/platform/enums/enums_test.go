package enums

import "testing"

// F01 fixes both the six components and their order, and the order is the default position on creation.
func TestPromptComponentTypesMatchF01(t *testing.T) {
	want := []PromptComponentType{"objective", "personality", "behavior", "tone", "brand_voice", "rules"}

	if len(PromptComponentTypeValues) != len(want) {
		t.Fatalf("PromptComponentTypeValues: got %d values, want %d", len(PromptComponentTypeValues), len(want))
	}
	for i, value := range want {
		if PromptComponentTypeValues[i] != value {
			t.Fatalf("PromptComponentTypeValues[%d]: got %q, want %q", i, PromptComponentTypeValues[i], value)
		}
	}
}

// The three modes of F02 and F27, with off quoted in the contract because YAML would read it as a boolean.
func TestAgentModeCoversF02(t *testing.T) {
	for _, mode := range []AgentMode{"autonomous", "suggestion", "off"} {
		if !mode.IsValid() {
			t.Fatalf("AgentMode(%q).IsValid(): got false, want true", mode)
		}
	}
	if AgentMode("agent").IsValid() {
		t.Fatal(`AgentMode("agent").IsValid(): got true, want false`)
	}
}

// C07 exists because "resolved", "Resolved" and "resuelto" can otherwise coexist and a metric counts wrong without failing.
func TestIsValidRejectsWhatTheContractDoesNotDeclare(t *testing.T) {
	cases := []struct {
		name  string
		valid bool
	}{
		{name: "resolved", valid: true},
		{name: "Resolved", valid: false},
		{name: "resuelto", valid: false},
		{name: "", valid: false},
		{name: "client", valid: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ConversationStatus(tc.name).IsValid(); got != tc.valid {
				t.Fatalf("ConversationStatus(%q).IsValid(): got %v, want %v", tc.name, got, tc.valid)
			}
		})
	}
}

// GenerationStatus describes the filtering and MessageStatus the delivery, so sharing a value would make both ambiguous.
func TestGenerationStatusDoesNotReuseTheValueOfMessageStatus(t *testing.T) {
	if GenerationStatus("sent").IsValid() {
		t.Fatal(`GenerationStatus("sent").IsValid(): got true, want false: it was renamed to auto_sent`)
	}
	if !GenerationStatus("auto_sent").IsValid() {
		t.Fatal(`GenerationStatus("auto_sent").IsValid(): got false, want true`)
	}
}
