package backend

import "testing"

func TestOutputPreservesWhitespaceAroundTranslation(t *testing.T) {
	for _, text := range []string{"\n  Translation\t\n", "  Translation", "Translation\r\n"} {
		got, err := cleanOutput(text)
		if err != nil || got != text {
			t.Fatalf("output %q = %q, %v", text, got, err)
		}
	}
	got, err := cleanOutput("\nTranslation\n<|plamo:tag|>\n")
	if err != nil || got != "\nTranslation\n" {
		t.Fatalf("stop marker cleanup = %q, %v", got, err)
	}
}
