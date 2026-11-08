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

	authService := auth.NewService(deps.DB)
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
	protected.Handle("GET /app", web.RequireAuth(webHandler.Dashboard))
	protected.Handle("GET /app/households/new", web.RequireAuth(webHandler.NewHouseholdPage))
	protected.Handle("POST /app/households", web.RequireAuth(webHandler.CreateHousehold))
	protected.Handle("GET /app/households/{householdID}/settings", web.RequireAuth(webHandler.HouseholdSettings))
	protected.Handle("POST /app/households/{householdID}/settings", web.RequireAuth(webHandler.UpdateHousehold))
	protected.Handle("POST /app/active-household", web.RequireAuth(webHandler.ActivateHousehold))
	protected.Handle("GET /app/preferences", web.RequireAuth(webHandler.PreferencesPage))
	protected.Handle("POST /app/preferences", web.RequireAuth(webHandler.UpdatePreferences))
	protected.Handle("POST /app/households/{householdID}/options", web.RequireAuth(webHandler.UpdateHouseholdOptions))
	protected.Handle("POST /app/households/{householdID}/invitations", web.RequireAuth(webHandler.CreateHouseholdInvitation))
	protected.Handle("POST /app/households/{householdID}/invitations/{invitationID}/delete", web.RequireAuth(webHandler.RevokeHouseholdInvitation))
	protected.Handle("GET /invitations/{token}", http.HandlerFunc(webHandler.InvitationPage))
	protected.Handle("POST /invitations/{token}", http.HandlerFunc(webHandler.AcceptInvitation))
	protected.Handle("GET /app/entries", web.RequireAuth(webHandler.Entries))
	protected.Handle("GET /app/exchange-rates", web.RequireAuth(webHandler.ExchangeRatesPage))
	protected.Handle("POST /app/exchange-rates", web.RequireAuth(webHandler.CreateExchangeRate))
	protected.Handle("GET /app/exchange-rates/{rateID}/edit", web.RequireAuth(webHandler.EditExchangeRatePage))
	protected.Handle("POST /app/exchange-rates/{rateID}", web.RequireAuth(webHandler.UpdateExchangeRate))
	protected.Handle("POST /app/exchange-rates/{rateID}/delete", web.RequireAuth(webHandler.DeleteExchangeRate))
	protected.Handle("GET /app/reports", web.RequireAuth(webHandler.ReportsPage))
	protected.Handle("GET /app/incomes/new", web.RequireAuth(webHandler.NewIncomePage))
	protected.Handle("POST /app/incomes", web.RequireAuth(webHandler.CreateIncome))
	protected.Handle("GET /app/incomes/{entryID}/edit", web.RequireAuth(webHandler.EditIncomePage))
	protected.Handle("POST /app/incomes/{entryID}", web.RequireAuth(webHandler.UpdateIncome))
	protected.Handle("POST /app/incomes/{entryID}/delete", web.RequireAuth(webHandler.DeleteIncome))
	protected.Handle("GET /app/spendings/new", web.RequireAuth(webHandler.NewSpendingPage))
	protected.Handle("POST /app/spendings", web.RequireAuth(webHandler.CreateSpending))
	protected.Handle("GET /app/spendings/{entryID}/edit", web.RequireAuth(webHandler.EditSpendingPage))
	protected.Handle("POST /app/spendings/{entryID}", web.RequireAuth(webHandler.UpdateSpending))
	protected.Handle("POST /app/spendings/{entryID}/delete", web.RequireAuth(webHandler.DeleteSpending))
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
	mux.Handle("/", sessions.Load(csrf.Protect(protected)))

	handler := httpapi.RequestID(
		httpapi.SecurityHeaders(httpapi.SecurityOptions{Production: deps.SecureCookies})(
			httpapi.RequestLogger(deps.Logger)(
				httpapi.Recoverer(deps.Logger)(mux),
			),
		),
	)
	return handler, nil
}
