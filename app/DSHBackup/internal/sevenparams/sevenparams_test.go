package sevenparams

import "testing"

func TestValidateExtra(t *testing.T) {
	valid := []string{"-md=128m", "-mmt=4", "-mfb=273", "-ms=on", "-mqs=off"}
	if _, err := ValidateExtra(valid); err != nil {
		t.Fatalf("expected valid args: %v", err)
	}
	invalid := []string{"-psecret", "-oC:\\Windows", "@/tmp/list", "-wC:\\", "-mhe=off", "-mx=0", "-mx=10", "-mmt=0", "-md=7m", "-sdel"}
	for _, arg := range invalid {
		if _, err := ValidateExtra([]string{arg}); err == nil {
			t.Fatalf("expected %q to be rejected", arg)
		}
	}
}

func TestLevelsAndArgs(t *testing.T) {
	if LevelMX("store") != 0 || LevelMX("fast") != 1 || LevelMX("quick") != 3 || LevelMX("balanced") != 5 || LevelMX("high") != 7 || LevelMX("max") != 9 {
		t.Fatal("unexpected compression levels")
	}
	if LevelForMX(0) != "store" || LevelForMX(2) != "quick" || LevelForMX(8) != "high" || LevelForMX(9) != "max" {
		t.Fatal("unexpected legacy level migration")
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
