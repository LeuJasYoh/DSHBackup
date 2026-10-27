package sevenparams

import "testing"

func TestValidateExtra(t *testing.T) {
	valid := []string{"-mx=9", "-md=128m", "-mmt=4", "-mfb=273", "-ms=on", "-mqs=off"}
	if _, err := ValidateExtra(valid); err != nil {
		t.Fatalf("expected valid args: %v", err)
	}
	invalid := []string{"-psecret", "-oC:\\Windows", "@/tmp/list", "-wC:\\", "-mhe=off", "-mx=10", "-mmt=0", "-md=7m", "-sdel"}
	for _, arg := range invalid {
		if _, err := ValidateExtra([]string{arg}); err == nil {
			t.Fatalf("expected %q to be rejected", arg)
		}
	}
}

func TestLevelsAndArgs(t *testing.T) {
	if LevelMX("fast") != 1 || LevelMX("balanced") != 5 || LevelMX("max") != 9 {
		t.Fatal("unexpected compression levels")
	}
	args := BuildArgs("balanced", 64, 4, []string{"-mfb=273"})
	expected := []string{"-mx=5", "-md=64m", "-mmt=4", "-mfb=273"}
	if len(args) != len(expected) {
		t.Fatalf("unexpected args: %v", args)
	}
	for index := range expected {
		if args[index] != expected[index] {
			t.Fatalf("unexpected args: %v", args)
		}
	}
}
