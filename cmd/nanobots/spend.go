// `nanobots spend` — what 1Claw has billed this account for model tokens.
package main

import "fmt"

// runSpend answers the question a scheduled product has to be able to
// answer: what did last night cost.
func runSpend(args []string) error {
	if err := rejectUnknown("spend", args); err != nil {
		return err
	}
	oc, err := oneClawClient()
	if err != nil {
		return err
	}
	b, err := oc.LLMTokenBillingStatus()
	if err != nil {
		return err
	}
	if !b.Enabled {
		fmt.Println("1Claw is not billing this account's model tokens, so it has no figure to report.")
		fmt.Println("Model spend on a direct provider key is between you and that provider.")
		return nil
	}
	if cents, known := b.Spent(); known {
		fmt.Printf("this billing period: $%.2f\n", float64(cents)/100)
		if b.CycleUsage.PeriodStart != "" {
			fmt.Printf("  since %s\n", b.CycleUsage.PeriodStart)
		}
	} else {
		fmt.Println("this billing period: nothing metered yet")
	}
	if cb := b.CreditBalance; cb != nil && (cb.AvailableCents > 0 || cb.UsedCents > 0) {
		fmt.Printf("credit: $%.2f available, $%.2f used\n",
			float64(cb.AvailableCents)/100, float64(cb.UsedCents)/100)
	}
	if b.Warning != "" {
		fmt.Printf("\n%s\n", b.Warning)
	}
	return nil
}
