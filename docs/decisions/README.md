# Architecture decision records

Short records capture consequential decisions. They describe the context at the
time of the decision and can be superseded by a later record.

| Number | Decision | Status |
| --- | --- | --- |
| [0001](0001-server-rendered-go-application.md) | One Go executable with server-rendered HTML and a versioned JSON API | Accepted |
| [0002](0002-postgresql-and-explicit-migrations.md) | PostgreSQL with `pgx` and explicit SQL migrations | Accepted |
| [0003](0003-money-and-exchange-rate-representation.md) | Integer minor units for money and exact decimals for exchange rates | Accepted |
| [0004](0004-authentication-and-household-authorization.md) | Separate authentication data and household-scoped authorization | Accepted |

## Convention

Use `NNNN-short-title.md` and include status, context, decision, and
consequences. Update the status when a record is superseded and link to the
replacement.
