-- Entry forms order currency choices by the actor's most recently recorded
-- entries. The existing (household_id, user_id, entry_date) indexes scan that
-- actor's whole history and sort by created_at before applying the 20-row
-- window. These indexes let the recents query stop after the window instead.
--
-- Keys ascend because new entries extend the end of their (household_id,
-- user_id) range. When that range sorts last, PostgreSQL appends without
-- repeated page splits, which is what the measurement captured: 33 MB
-- ascending against 93 MB for a DESC variant on the same 500k-row,
-- single-household workload. The benefit depends on where the range falls in
-- the index, so it is not a general guarantee. Ascending keys keep the
-- measured result, and ORDER BY created_at DESC, id DESC still reads them with
-- a backward index scan.
CREATE INDEX incomes_household_user_created_idx
    ON incomes (household_id, user_id, created_at, id);

CREATE INDEX spendings_household_user_created_idx
    ON spendings (household_id, user_id, created_at, id);
