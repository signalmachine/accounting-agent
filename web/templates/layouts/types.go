package layouts

// AppLayoutData is passed to AppLayout to configure the page shell.
type AppLayoutData struct {
	Title        string
	CompanyName  string
	CompanyCode  string
	FYBadge      string
	Username     string
	Role         string
	ActiveNav    string // e.g. "ai-agent", "dashboard", "customers", "orders", "trial-balance"
	FlashMsg     string
	FlashKind    string // "success", "error", "warning", "info"
	FlushContent bool   // removes main padding and switches to flex-col for full-height pages (e.g. AI chat)
}
