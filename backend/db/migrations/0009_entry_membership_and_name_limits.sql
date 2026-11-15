-- Entries belong to a household and are either household-wide or attributed to
-- a member of that household. Tying (household_id, user_id) to
-- household_memberships makes that invariant hold in the schema instead of
-- relying on the service layer alone.
--
-- Household-wide entries store a NULL user_id, and SQL MATCH SIMPLE (the
-- default) exempts any foreign key whose referencing columns are not all
-- non-null, so those rows stay valid without a second constraint.
--
-- ON DELETE CASCADE mirrors the existing cascade from users to their entries.
-- No member-removal feature exists yet (decision Q5), so the delete behavior is
-- chosen for consistency with the current users cascade rather than for a
-- shipped workflow.
--
-- Both constraints are added NOT VALID and then validated in a separate
-- statement: the validation takes only a SHARE UPDATE EXCLUSIVE lock, so an
-- existing database can be checked without blocking concurrent writes for the
-- duration of a full table scan.
ALTER TABLE incomes
    ADD CONSTRAINT incomes_membership_fk
        FOREIGN KEY (household_id, user_id)
        REFERENCES household_memberships (household_id, user_id) ON DELETE CASCADE
        NOT VALID;

ALTER TABLE incomes VALIDATE CONSTRAINT incomes_membership_fk;

ALTER TABLE spendings
    ADD CONSTRAINT spendings_membership_fk
        FOREIGN KEY (household_id, user_id)
        REFERENCES household_memberships (household_id, user_id) ON DELETE CASCADE
        NOT VALID;

ALTER TABLE spendings VALIDATE CONSTRAINT spendings_membership_fk;

-- users.first_name and users.last_name mirror the 80-character application
-- limit in auth.validatePersonName. The existing inline checks keep rejecting
-- blank names; these add the trimmed length limit with the same NOT VALID plus
-- VALIDATE pattern used above.
ALTER TABLE users
    ADD CONSTRAINT users_first_name_length CHECK (char_length(btrim(first_name)) <= 80) NOT VALID;

ALTER TABLE users VALIDATE CONSTRAINT users_first_name_length;

ALTER TABLE users
    ADD CONSTRAINT users_last_name_length CHECK (char_length(btrim(last_name)) <= 80) NOT VALID;

ALTER TABLE users VALIDATE CONSTRAINT users_last_name_length;
