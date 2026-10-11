package keyboard

import "testing"

func TestActionRoundTrip(t *testing.T) {
	a := Action{Do: "merge", Repo: "me/app", Number: 42}
	if a.Data() != "merge:me/app:42" {
		t.Fatalf("Data = %q", a.Data())
	}
	got, ok := Parse(a.Data())
	if !ok || got != a {
		t.Fatalf("Parse = %+v, %v", got, ok)
	}
	if len(a.Data()) > 64 {
		t.Fatal("Telegram ограничивает данные кнопки 64 байтами")
	}
	for _, bad := range []string{"", "merge", "merge:me/app:x"} {
		if _, ok := Parse(bad); ok {
			t.Fatalf("Parse(%q) должен отказать", bad)
		}
	}
}
