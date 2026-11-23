// Package server assembles the HTTP application from its adapters and services.
package server

import (
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"budgettracker/internal/auth"
	"budgettracker/internal/finance"
	"budgettracker/internal/households"
	"budgettracker/internal/httpapi"
	"budgettracker/internal/reporting"
	"budgettracker/internal/web"
)

// Deps contains the dependencies needed to build the HTTP handler.
type Deps struct {
	Logger        *slog.Logger
	DB            *pgxpool.Pool
	StaticDir     string
	SecureCookies bool
}

// New constructs the complete HTTP handler.
func New(deps Deps) (http.Handler, error) {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}

	authService := auth.NewService(deps.DB, deps.Logger)
	householdService := households.NewService(deps.DB)
	financeService := finance.NewService(deps.DB)
	reportService := reporting.NewService(deps.DB, financeService)
	cookies := auth.CookieConfig{Secure: deps.SecureCookies}
	csrf := httpapi.NewCSRF(httpapi.CSRFConfig{Secure: deps.SecureCookies})
	sessions := auth.NewMiddleware(authService, cookies, deps.Logger)

	webHandler, err := web.New(web.Deps{
		Logger:     deps.Logger,
		Sessions:   authService,
		Households: householdService,
		Entries:    financeService,
		Rates:      financeService,
		Reports:    reportService,
		Cookies:    cookies,
		StaticDir:  deps.StaticDir,
	})
	if err != nil {
		return nil, err
	}
	health := httpapi.NewHealth(deps.DB, deps.Logger)
	householdAPI := httpapi.NewHouseholdHandler(householdService, deps.Logger)
	entryAPI := httpapi.NewEntryHandler(financeService, deps.Logger)
	rateAPI := httpapi.NewExchangeRateHandler(financeService, deps.Logger)
	reportAPI := httpapi.NewReportHandler(reportService, deps.Logger)

	protected := http.NewServeMux()
	protected.Handle("GET /{$}", http.HandlerFunc(webHandler.Home))
	protected.Handle("GET /login", http.HandlerFunc(webHandler.LoginPage))
	protected.Handle("POST /login", http.HandlerFunc(webHandler.Login))
	protected.Handle("GET /register", http.HandlerFunc(webHandler.RegisterPage))
	protected.Handle("POST /register", http.HandlerFunc(webHandler.Register))
	protected.Handle("POST /logout", web.RequireAuth(webHandler.Logout))
	protected.Handle("GET /app", web.RequireAuth(webHandler.App))
	protected.HandleFunc("GET /app/{householdID}", webHandler.Scoped("entries", "", webHandler.HouseholdHome))
	protected.Handle("GET /app/households/new", web.RequireAuth(webHandler.NewHouseholdPage))
	protected.Handle("POST /app/households", web.RequireAuth(webHandler.CreateHousehold))
	protected.HandleFunc("GET /app/{householdID}/settings", webHandler.Scoped("more", "settings", webHandler.HouseholdSettings))
	protected.HandleFunc("POST /app/{householdID}/settings", webHandler.Scoped("more", "", webHandler.UpdateHousehold))
	protected.Handle("POST /app/active-household", web.RequireAuth(webHandler.ActivateHousehold))
	protected.HandleFunc("GET /app/{householdID}/preferences", webHandler.Scoped("more", "preferences", webHandler.PreferencesPage))
	protected.HandleFunc("POST /app/{householdID}/preferences", webHandler.Scoped("more", "", webHandler.UpdatePreferences))
	protected.HandleFunc("POST /app/{householdID}/settings/options", webHandler.Scoped("more", "", webHandler.UpdateHouseholdOptions))
	protected.HandleFunc("POST /app/{householdID}/settings/invitations", webHandler.Scoped("more", "", webHandler.CreateHouseholdInvitation))
	protected.HandleFunc("POST /app/{householdID}/settings/invitations/{invitationID}/delete", webHandler.Scoped("more", "", webHandler.RevokeHouseholdInvitation))
	protected.Handle("GET /invitations/{token}", http.HandlerFunc(webHandler.InvitationPage))
	protected.Handle("POST /invitations/{token}", http.HandlerFunc(webHandler.AcceptInvitation))
	protected.HandleFunc("GET /app/{householdID}/entries", webHandler.Scoped("entries", "entries", webHandler.Entries))
	protected.HandleFunc("GET /app/{householdID}/exchange-rates", webHandler.Scoped("more", "exchange-rates", webHandler.ExchangeRatesPage))
	protected.HandleFunc("POST /app/{householdID}/exchange-rates", webHandler.Scoped("more", "", webHandler.CreateExchangeRate))
	protected.HandleFunc("GET /app/{householdID}/exchange-rates/{rateID}/edit", webHandler.Scoped("more", "", webHandler.EditExchangeRatePage))
	protected.HandleFunc("POST /app/{householdID}/exchange-rates/{rateID}", webHandler.Scoped("more", "", webHandler.UpdateExchangeRate))
	protected.HandleFunc("POST /app/{householdID}/exchange-rates/{rateID}/delete", webHandler.Scoped("more", "", webHandler.DeleteExchangeRate))
	protected.HandleFunc("GET /app/{householdID}/reports", webHandler.Scoped("reports", "reports", webHandler.ReportsPage))
	protected.HandleFunc("GET /app/{householdID}/incomes/new", webHandler.Scoped("entries", "", webHandler.NewIncomePage))
	protected.HandleFunc("POST /app/{householdID}/incomes", webHandler.Scoped("entries", "", webHandler.CreateIncome))
	protected.HandleFunc("GET /app/{householdID}/incomes/{entryID}/edit", webHandler.Scoped("entries", "", webHandler.EditIncomePage))
	protected.HandleFunc("POST /app/{householdID}/incomes/{entryID}", webHandler.Scoped("entries", "", webHandler.UpdateIncome))
	protected.HandleFunc("POST /app/{householdID}/incomes/{entryID}/delete", webHandler.Scoped("entries", "", webHandler.DeleteIncome))
	protected.HandleFunc("GET /app/{householdID}/spendings/new", webHandler.Scoped("entries", "", webHandler.NewSpendingPage))
	protected.HandleFunc("POST /app/{householdID}/spendings", webHandler.Scoped("entries", "", webHandler.CreateSpending))
	protected.HandleFunc("GET /app/{householdID}/spendings/{entryID}/edit", webHandler.Scoped("entries", "", webHandler.EditSpendingPage))
	protected.HandleFunc("POST /app/{householdID}/spendings/{entryID}", webHandler.Scoped("entries", "", webHandler.UpdateSpending))
	protected.HandleFunc("POST /app/{householdID}/spendings/{entryID}/delete", webHandler.Scoped("entries", "", webHandler.DeleteSpending))
	protected.Handle("GET /api/v1/households", httpapi.RequireAuthAPI(householdAPI.List))
	protected.Handle("POST /api/v1/households", httpapi.RequireAuthAPI(householdAPI.Create))
	protected.Handle("GET /api/v1/households/{householdID}", httpapi.RequireAuthAPI(householdAPI.Get))
	protected.Handle("GET /api/v1/households/{householdID}/members", httpapi.RequireAuthAPI(householdAPI.Members))
	protected.Handle("GET /api/v1/households/{householdID}/options", httpapi.RequireAuthAPI(householdAPI.Options))
	protected.Handle("PUT /api/v1/households/{householdID}/options", httpapi.RequireAuthAPI(householdAPI.UpdateOptions))
	protected.Handle("GET /api/v1/households/{householdID}/my-options", httpapi.RequireAuthAPI(householdAPI.MyOptions))
	protected.Handle("PUT /api/v1/households/{householdID}/my-options", httpapi.RequireAuthAPI(householdAPI.UpdateMyOptions))
	protected.Handle("GET /api/v1/households/{householdID}/invitations", httpapi.RequireAuthAPI(householdAPI.ListInvitations))
	protected.Handle("POST /api/v1/households/{householdID}/invitations", httpapi.RequireAuthAPI(householdAPI.CreateInvitation))
	protected.Handle("DELETE /api/v1/households/{householdID}/invitations/{invitationID}", httpapi.RequireAuthAPI(householdAPI.RevokeInvitation))
	protected.Handle("GET /api/v1/currencies", httpapi.RequireAuthAPI(entryAPI.Currencies))
	protected.Handle("GET /api/v1/incomes", httpapi.RequireAuthAPI(entryAPI.ListIncome))
	protected.Handle("POST /api/v1/incomes", httpapi.RequireAuthAPI(entryAPI.CreateIncome))
	protected.Handle("GET /api/v1/incomes/{entryID}", httpapi.RequireAuthAPI(entryAPI.GetIncome))
	protected.Handle("PATCH /api/v1/incomes/{entryID}", httpapi.RequireAuthAPI(entryAPI.UpdateIncome))
	protected.Handle("DELETE /api/v1/incomes/{entryID}", httpapi.RequireAuthAPI(entryAPI.DeleteIncome))
	protected.Handle("GET /api/v1/spendings", httpapi.RequireAuthAPI(entryAPI.ListSpending))
	protected.Handle("POST /api/v1/spendings", httpapi.RequireAuthAPI(entryAPI.CreateSpending))
	protected.Handle("GET /api/v1/spendings/{entryID}", httpapi.RequireAuthAPI(entryAPI.GetSpending))
	protected.Handle("PATCH /api/v1/spendings/{entryID}", httpapi.RequireAuthAPI(entryAPI.UpdateSpending))
	protected.Handle("DELETE /api/v1/spendings/{entryID}", httpapi.RequireAuthAPI(entryAPI.DeleteSpending))
	protected.Handle("GET /api/v1/exchange-rates", httpapi.RequireAuthAPI(rateAPI.List))
	protected.Handle("POST /api/v1/exchange-rates", httpapi.RequireAuthAPI(rateAPI.Create))
	protected.Handle("PATCH /api/v1/exchange-rates/{rateID}", httpapi.RequireAuthAPI(rateAPI.Update))
	protected.Handle("DELETE /api/v1/exchange-rates/{rateID}", httpapi.RequireAuthAPI(rateAPI.Delete))
	protected.Handle("GET /api/v1/reports/summary", httpapi.RequireAuthAPI(reportAPI.Summary))

	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", webHandler.Static()))
	mux.Handle("GET /healthz", http.HandlerFunc(health.Live))
	mux.Handle("GET /readyz", http.HandlerFunc(health.Ready))
	mux.Handle("/", sessions.Load(httpapi.LimitHTMLFormBody(csrf.Protect(protected))))

	handler := httpapi.RequestID(
		httpapi.SecurityHeaders(httpapi.SecurityOptions{Production: deps.SecureCookies})(
			httpapi.RequestLogger(deps.Logger)(
				httpapi.Recoverer(deps.Logger)(mux),
			),
		),
	)
	return handler, nil
}
