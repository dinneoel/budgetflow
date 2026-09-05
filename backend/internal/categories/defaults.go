package categories

// DefaultGroup describes one seeded category group and its categories.
type DefaultGroup struct {
	Name       string
	Categories []DefaultCategory
}

// DefaultCategory describes one seeded category.
type DefaultCategory struct {
	Name         string
	Icon         string
	Color        string
	BudgetType   string
	RolloverRule string
}

// DefaultGroups is the PRD default set seeded for new budgets. Order here is
// the seeded sort order.
var DefaultGroups = []DefaultGroup{
	{Name: "Bills & Utilities", Categories: []DefaultCategory{
		{Name: "Rent / Mortgage", Icon: "home", Color: "#0ea5e9", BudgetType: "fixed", RolloverRule: "none"},
		{Name: "Electricity", Icon: "zap", Color: "#f59e0b", BudgetType: "variable", RolloverRule: "none"},
		{Name: "Water", Icon: "droplet", Color: "#38bdf8", BudgetType: "variable", RolloverRule: "none"},
		{Name: "Internet", Icon: "wifi", Color: "#6366f1", BudgetType: "fixed", RolloverRule: "none"},
		{Name: "Phone", Icon: "phone", Color: "#8b5cf6", BudgetType: "fixed", RolloverRule: "none"},
	}},
	{Name: "Everyday Spending", Categories: []DefaultCategory{
		{Name: "Groceries", Icon: "shopping-cart", Color: "#22c55e", BudgetType: "variable", RolloverRule: "none"},
		{Name: "Transportation", Icon: "bus", Color: "#14b8a6", BudgetType: "variable", RolloverRule: "none"},
		{Name: "Dining Out", Icon: "utensils", Color: "#f97316", BudgetType: "variable", RolloverRule: "none"},
		{Name: "Personal Care", Icon: "heart", Color: "#ec4899", BudgetType: "variable", RolloverRule: "none"},
		{Name: "Household Supplies", Icon: "package", Color: "#a3a3a3", BudgetType: "variable", RolloverRule: "none"},
	}},
	{Name: "Lifestyle", Categories: []DefaultCategory{
		{Name: "Entertainment", Icon: "film", Color: "#e11d48", BudgetType: "variable", RolloverRule: "none"},
		{Name: "Subscriptions", Icon: "repeat", Color: "#7c3aed", BudgetType: "fixed", RolloverRule: "none"},
		{Name: "Shopping", Icon: "shopping-bag", Color: "#d946ef", BudgetType: "variable", RolloverRule: "none"},
		{Name: "Travel", Icon: "plane", Color: "#0891b2", BudgetType: "sinking_fund", RolloverRule: "rollover"},
		{Name: "Gifts", Icon: "gift", Color: "#dc2626", BudgetType: "sinking_fund", RolloverRule: "rollover"},
	}},
	{Name: "Health & Education", Categories: []DefaultCategory{
		{Name: "Medical", Icon: "stethoscope", Color: "#ef4444", BudgetType: "variable", RolloverRule: "rollover"},
		{Name: "Insurance", Icon: "shield", Color: "#64748b", BudgetType: "fixed", RolloverRule: "none"},
		{Name: "Education", Icon: "book", Color: "#2563eb", BudgetType: "variable", RolloverRule: "none"},
	}},
	{Name: "Savings & Debt", Categories: []DefaultCategory{
		{Name: "Emergency Fund", Icon: "life-buoy", Color: "#16a34a", BudgetType: "savings_goal", RolloverRule: "rollover"},
		{Name: "Debt Payments", Icon: "credit-card", Color: "#b91c1c", BudgetType: "debt", RolloverRule: "none"},
		{Name: "Long-term Savings", Icon: "piggy-bank", Color: "#15803d", BudgetType: "savings_goal", RolloverRule: "rollover"},
	}},
}
