# iOS date input sizing

Status: In progress

The [mobile-first UI plan](./20261122_190603_mobile_first_ui.md) passed desktop and emulated mobile smoke checks but did not cover physical devices. A real iPhone running Brave showed the date input extending past the form card while the adjacent controls fit. Chrome's responsive preview did not reproduce it.

- **Implemented:** Normalize date input appearance and bound the border box to its container in the shared stylesheet. Keep native `type="date"` inputs and their existing form values; no JavaScript or backend changes.
- **Validated:** `mise run check` and `mise run test:integration` passed. A browser smoke fixture using the compiled stylesheet verified that filled and empty date inputs fit at 320 and 390 px in emulated WebKit and at 1280 px in Chrome, and still submit ISO date values.
- **Pending device acceptance:** On a real iPhone, confirm that date controls fit in the entry and exchange-rate forms and the Entries and Reports filters, and that tapping a field still opens the native picker and saves or applies the selected date.
