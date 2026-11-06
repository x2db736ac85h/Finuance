package assistant

import (
	"encoding/json"
	"strings"

	"github.com/x2db736ac85h/finuance/backend/internal/llm"
)

// The tool definitions are written as JSON text in the legacy's key order (FILTERS, AI_TOOLS), so
// plan mode prints them exactly as the legacy PLAN() did and the schemas stay readable here.

// filters is legacy FILTERS.
const filters = `"query":{"type":"string","description":"Words that must ALL appear somewhere in the transaction (payee, title, address, category…). Case- and accent-insensitive."},` +
	`"payee":{"type":"string","description":"Text contained in the payee/merchant name"},` +
	`"category":{"type":"string","description":"Category name or part of it"},` +
	`"group":{"type":"string","description":"Name of a user-defined transaction group (e.g. a trip)"},` +
	`"date_from":{"type":"string","description":"Inclusive start date, YYYY-MM-DD"},` +
	`"date_to":{"type":"string","description":"Inclusive end date, YYYY-MM-DD"},` +
	`"type":{"type":"string","enum":["expense","income","any"],"description":"expense = money spent (negative amounts), income = money received"},` +
	`"min_amount":{"type":"number","description":"Minimum absolute amount"},` +
	`"max_amount":{"type":"number","description":"Maximum absolute amount"}`

var toolDefs = []llm.Tool{
	{Name: ToolSearch, Description: "List individual transactions that match filters. Returns the matched count, totals and up to `limit` rows.",
		Parameters: json.RawMessage(`{"type":"object","properties":{` + filters +
			`,"sort":{"type":"string","enum":["date_desc","date_asc","amount_desc","amount_asc"]},"limit":{"type":"integer","description":"Rows to return (default 25)"}}}`)},
	{Name: ToolAggregate, Description: `Counts and totals (spent, income, net) grouped by a dimension. Use for "how much", "how many", trends, rankings and comparisons.`,
		Parameters: json.RawMessage(`{"type":"object","properties":{"group_by":{"type":"string","enum":["none","category","payee","month","week","day","weekday","type","group","account"]},` +
			filters + `,"top":{"type":"integer","description":"Max groups to return"}},"required":["group_by"]}`)},
	{Name: ToolValues, Description: "Distinct payee, category or group names containing some text, with counts. Use it to resolve how a merchant or category is spelled in the data.",
		Parameters: json.RawMessage(`{"type":"object","properties":{"field":{"type":"string","enum":["payee","category","group"]},"query":{"type":"string"}},"required":["field"]}`)},
	{Name: ToolMonth, Description: "One month at a glance: spent, income, net, savings rate, the projection for an unfinished month, the previous month, " +
		"spending per category with budgets and status, income sources and the largest expenses. Use for month summaries and budget questions.",
		Parameters: json.RawMessage(`{"type":"object","properties":{"month":{"type":"string","description":"YYYY-MM; default the latest month with data"}}}`)},
	{Name: ToolRecurring, Description: "Subscriptions and regular bills found in the data (the same payee charging a similar amount every week, month or year), " +
		"with the cadence, typical amount, yearly cost, last and next expected charge.",
		Parameters: json.RawMessage(`{"type":"object","properties":{"active_only":{"type":"boolean","description":"Only payments still charging (the last charge is recent for its cadence)"}}}`)},
	{Name: ToolBudgets, Description: "Budget status for one month: each category's spending against its budget (set by the user or the default from earlier months), " +
		"what is left and whether it is on track, ahead of pace or over. Use for \"am I over budget\" questions.",
		Parameters: json.RawMessage(`{"type":"object","properties":{"month":{"type":"string","description":"YYYY-MM; default the latest month with data"}}}`)},
	{Name: ToolGroup, Description: "Summary of one user-defined transaction group (e.g. a trip): totals, first and last day, spending per day, " +
		"the group budget and what is left, categories and top payees.",
		Parameters: json.RawMessage(`{"type":"object","properties":{"group":{"type":"string","description":"The group name (find_values with field group lists them)"}},"required":["group"]}`)},
	{Name: ToolGoals, Description: "The user's goals with their progress: saving an amount by a deadline (saved so far against the steady pace) " +
		"or spending a percentage less on a category (this month against the goal's monthly limit). Default every open goal.",
		Parameters: json.RawMessage(`{"type":"object","properties":{"goal":{"type":"string","description":"One goal's name; default every open goal"}}}`)},
	{Name: ToolReceipt, Description: "Find the transaction a receipt belongs to: money out of exactly the amount paid, booked within 3 days of the " +
		"receipt's date, nearest first, with how many words of the shop's name appear in each payee. Use after reading a receipt photo.",
		Parameters: json.RawMessage(`{"type":"object","properties":{"amount":{"type":"number","description":"The total paid (positive)"},` +
			`"date":{"type":"string","description":"The receipt's date, YYYY-MM-DD"},"shop":{"type":"string","description":"The shop's name as printed"},` +
			`"currency":{"type":"string","description":"ISO 4217 code, e.g. EUR"}},"required":["amount","date"]}`)},
	{Name: ToolPropose, Description: "Propose a change for the user to confirm: put transactions in a category (set_category) or a group (set_group), " +
		"create a rule that categorises a payee from now on (create_rule), set or remove a category's monthly budget (set_budget), " +
		"type a cash payment into a cash account (add_expense), share transactions with a shared group (add_to_shared_group), " +
		"change how one of the user's expenses in a shared group is split (split_expense), " +
		"set a goal (create_goal: save an amount by a deadline, or spend reduce_pct % less on a category) or change one (update_goal), " +
		"or write a receipt into the note of the transaction find_receipt_match found (attach_receipt). " +
		"You never change data yourself: the user sees a card and applies it. Select transactions with the usual filters; a rule uses payee and/or query; " +
		"add_expense uses account, date, amount, payee and optionally to_category; split_expense uses shared_group, payee (and date), split_mode and members; " +
		"create_goal uses goal_name, deadline and amount (save) or to_category and reduce_pct (reduce); update_goal names the goal and what changes; " +
		"attach_receipt uses the receipt's amount, date, shop and items (and account when several fit).",
		Parameters: json.RawMessage(`{"type":"object","properties":{"action":{"type":"string","enum":["set_category","set_group","create_rule","set_budget","add_expense","add_to_shared_group","split_expense","create_goal","update_goal","attach_receipt"]},` +
			`"to_category":{"type":"string","description":"set_category, create_rule, set_budget, add_expense: the category name (an existing one, or a new one to create; set_budget needs an existing one); create_goal, update_goal: the existing category to spend less on"},` +
			`"to_group":{"type":"string","description":"set_group: the group name (an existing one, or a new one to create)"},` +
			`"rule_name":{"type":"string","description":"create_rule: a short name for the rule"},` +
			`"amount":{"type":"number","description":"set_budget: the monthly budget; add_expense, attach_receipt: the amount paid (positive); create_goal, update_goal: the amount to save"},` +
			`"remove_budget":{"type":"boolean","description":"set_budget: remove the category's own budget (back to the default)"},` +
			`"account":{"type":"string","description":"add_expense: the cash account's name (may be left out when there is one), or the bank account a card payment belongs to; create_goal: optionally the one account the savings go to; attach_receipt: the account of the matching payment"},` +
			`"shop":{"type":"string","description":"attach_receipt: the shop's name as printed on the receipt"},` +
			`"items":{"type":"string","description":"attach_receipt: what was bought, a few words"},` +
			`"currency":{"type":"string","description":"attach_receipt: the receipt's ISO 4217 currency code"},` +
			`"goal":{"type":"string","description":"update_goal: the goal's name"},` +
			`"goal_name":{"type":"string","description":"create_goal: a short name; update_goal: a new name"},` +
			`"goal_kind":{"type":"string","enum":["save","reduce"],"description":"create_goal: save an amount, or spend less on a category"},` +
			`"reduce_pct":{"type":"integer","description":"create_goal, update_goal (reduce): spend this many percent less than the average of the last 3 months"},` +
			`"deadline":{"type":"string","description":"create_goal, update_goal: the goal's last day, YYYY-MM-DD"},` +
			`"goal_status":{"type":"string","enum":["active","done","archived"],"description":"update_goal: close or reopen a goal"},` +
			`"date":{"type":"string","description":"add_expense: the day of the payment; attach_receipt: the receipt's date; YYYY-MM-DD"},` +
			`"note":{"type":"string","description":"add_expense: an optional note"},` +
			`"shared_group":{"type":"string","description":"add_to_shared_group, split_expense: the shared group's name"},` +
			`"split_mode":{"type":"string","enum":["equal","shares","exact"],"description":"split_expense: equal (one member: for someone else), by shares, or exact amounts"},` +
			`"members":{"type":"array","description":"split_expense: who shares it; \"me\" is the user. equal: [{\"name\":\"me\"},{\"name\":\"Bo\"}]; shares: add share; exact: add amount",` +
			`"items":{"type":"object","properties":{"name":{"type":"string"},"share":{"type":"integer"},"amount":{"type":"number"}},"required":["name"]}},` +
			`"reason":{"type":"string","description":"One short sentence for the user on why"},` + filters + `},"required":["action"]}`)},
}

// Tool sets: the full set, and the legacy three for small models and plan mode (PLAN §4.8; the
// evals decide the default per model).
const (
	ToolsetFull    = "full"
	ToolsetCompact = "compact"
)

// ToolDefs returns the definitions of a tool set.
func ToolDefs(set string) []llm.Tool {
	if set == ToolsetCompact {
		return toolDefs[:3]
	}
	return toolDefs
}

// PlanInstructions is the plan-mode suffix of the system prompt for a tool set (llm-probe).
func PlanInstructions(set string) string { return planInstructions(ToolDefs(set)) }

// planInstructions is legacy PLAN(): for models without tool calling, the tools are described in
// the prompt and the model answers with the calls as JSON.
func planInstructions(tools []llm.Tool) string {
	var b strings.Builder
	b.WriteString("\n\nYou cannot call functions directly in this mode. Reply with ONLY one JSON object, no other text:\n" +
		`{"calls":[{"name":"<tool name>","arguments":{...}}]}` + "\nUse {\"calls\":[]} if no data is needed. Available tools:\n[")
	for i, t := range tools {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"name":` + jsString(t.Name) + `,"description":` + jsString(t.Description) + `,"parameters":` + string(t.Parameters) + `}`)
	}
	b.WriteByte(']')
	return b.String()
}

// jsString is JSON.stringify of a string (no HTML escaping, unlike json.Marshal).
func jsString(s string) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}
