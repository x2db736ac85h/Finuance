-- Phase 19 (PR 3): shared groups (ADR 0028).
--
-- A shared group is a set of expenses several friends see: copies of what a member shares (date,
-- amount, payee, category name, note, who paid), never the member's accounts, bank descriptions
-- or raw rows. Splits say who owes what; settlements record payments between members.
--
-- Row-level security by membership. Policies ask shared_member_role(group) for the caller's role
-- in a group. The function must read shared_group_members without RLS: a policy that queries its
-- own table recurses, and FORCE RLS binds the table owner too. So shared_group_members alone has
-- RLS enabled but not forced (the app role finuance_app is still bound by it; only the owner
-- finuance_owner, which owns the function and runs migrations, is not). Every other shared table
-- keeps FORCE RLS. The schema test lists this one exception with its reason.
--
-- A deleted user's rows stay (their member row keeps the display name, user_id becomes NULL), so
-- the other members' balances don't move: they see a "Former member".

-- +goose Up
CREATE TABLE shared_groups (
  id           uuid PRIMARY KEY,
  owner_id     uuid REFERENCES users(id) ON DELETE SET NULL,
  name         text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 60),
  color        smallint NOT NULL DEFAULT 0 CHECK (color BETWEEN 0 AND 7),
  currency     char(3) NOT NULL,
  kind         text NOT NULL DEFAULT 'trip' CHECK (kind IN ('trip', 'household', 'event', 'other')),
  starts_on    date,
  ends_on      date,
  budget_minor bigint CHECK (budget_minor > 0),
  archived_at  timestamptz,
  created_at   timestamptz NOT NULL DEFAULT now(),
  CHECK (ends_on IS NULL OR starts_on IS NULL OR ends_on >= starts_on)
);
CREATE INDEX shared_groups_owner_idx ON shared_groups (owner_id);

CREATE TABLE shared_group_members (
  id            uuid PRIMARY KEY,
  group_id      uuid NOT NULL REFERENCES shared_groups(id) ON DELETE CASCADE,
  user_id       uuid REFERENCES users(id) ON DELETE SET NULL,  -- NULL: a former member (deleted user)
  role          text NOT NULL CHECK (role IN ('owner', 'member')),
  state         text NOT NULL CHECK (state IN ('invited', 'active', 'left')),
  display_name  text NOT NULL CHECK (char_length(display_name) BETWEEN 1 AND 80),
  invited_by    uuid REFERENCES users(id) ON DELETE SET NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  joined_at     timestamptz,
  left_at       timestamptz,
  UNIQUE (group_id, user_id),
  UNIQUE (group_id, id)
);
CREATE INDEX shared_group_members_user_idx ON shared_group_members (user_id);
CREATE INDEX shared_group_members_invited_by_idx ON shared_group_members (invited_by);

-- The caller's role in a group: 'owner' or 'member' while active, 'invited' while invited, NULL
-- otherwise (not a member, left, or no user set). SECURITY DEFINER so it reads the members table
-- without RLS (see the header); STABLE and with a fixed search_path.
CREATE FUNCTION shared_member_role(g uuid) RETURNS text
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog, public AS $$
  SELECT CASE m.state WHEN 'active' THEN m.role WHEN 'invited' THEN 'invited' END
  FROM public.shared_group_members m
  WHERE m.group_id = g AND m.user_id = nullif(current_setting('app.user_id', true), '')::uuid
$$;
REVOKE ALL ON FUNCTION shared_member_role(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION shared_member_role(uuid) TO finuance_app;

-- Member states only move forward: invited → active (accept) or left (decline, withdrawn);
-- active → left (leave, removed). A member never changes their own role.
-- +goose StatementBegin
CREATE FUNCTION shared_member_transition() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.role <> OLD.role OR NEW.group_id <> OLD.group_id OR NEW.id <> OLD.id THEN
    RAISE EXCEPTION 'a membership''s group and role are fixed' USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.user_id IS DISTINCT FROM OLD.user_id AND NEW.user_id IS NOT NULL THEN
    RAISE EXCEPTION 'a membership''s user is fixed' USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.state <> OLD.state AND NOT (
       (OLD.state = 'invited' AND NEW.state IN ('active', 'left'))
    OR (OLD.state = 'active' AND NEW.state = 'left')) THEN
    RAISE EXCEPTION 'membership state % cannot become %', OLD.state, NEW.state USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER shared_member_transition BEFORE UPDATE ON shared_group_members
  FOR EACH ROW EXECUTE FUNCTION shared_member_transition();

CREATE TABLE shared_expenses (
  id            uuid PRIMARY KEY,
  group_id      uuid NOT NULL REFERENCES shared_groups(id) ON DELETE CASCADE,
  added_by      uuid REFERENCES users(id) ON DELETE SET NULL,
  added_member  uuid NOT NULL,
  paid_by       uuid NOT NULL,
  booked_on     date NOT NULL,
  amount_minor  bigint NOT NULL CHECK (amount_minor <> 0),  -- what the group spent (negative: a refund)
  payee         text NOT NULL CHECK (char_length(payee) BETWEEN 1 AND 200),
  category_name text NOT NULL DEFAULT '' CHECK (char_length(category_name) <= 80),
  note          text NOT NULL DEFAULT '' CHECK (char_length(note) <= 2000),
  origin        text NOT NULL CHECK (origin IN ('transaction', 'typed')),
  split_mode    text NOT NULL DEFAULT 'equal' CHECK (split_mode IN ('equal', 'shares', 'exact')),
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (group_id, paid_by) REFERENCES shared_group_members (group_id, id),
  FOREIGN KEY (group_id, added_member) REFERENCES shared_group_members (group_id, id)
);
CREATE INDEX shared_expenses_group_idx ON shared_expenses (group_id, booked_on DESC);
CREATE INDEX shared_expenses_added_by_idx ON shared_expenses (added_by);
CREATE INDEX shared_expenses_paid_by_idx ON shared_expenses (group_id, paid_by);
CREATE INDEX shared_expenses_added_member_idx ON shared_expenses (group_id, added_member);

CREATE TABLE shared_expense_splits (
  expense_id   uuid NOT NULL REFERENCES shared_expenses(id) ON DELETE CASCADE,
  group_id     uuid NOT NULL,
  member_id    uuid NOT NULL,
  share_minor  bigint NOT NULL,
  weight       integer CHECK (weight > 0),  -- the member's shares in split mode 'shares'
  PRIMARY KEY (expense_id, member_id),
  FOREIGN KEY (group_id, member_id) REFERENCES shared_group_members (group_id, id)
);
CREATE INDEX shared_expense_splits_member_idx ON shared_expense_splits (group_id, member_id);

-- The splits of an expense always add up to its amount (checked when the transaction commits).
-- +goose StatementBegin
CREATE FUNCTION shared_split_sum() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  e uuid;
  amount bigint;
  total bigint;
BEGIN
  IF TG_TABLE_NAME = 'shared_expenses' THEN
    e := NEW.id;
  ELSIF TG_OP = 'DELETE' THEN
    e := OLD.expense_id;
  ELSE
    e := NEW.expense_id;
  END IF;
  SELECT x.amount_minor INTO amount FROM shared_expenses x WHERE x.id = e;
  IF NOT FOUND THEN
    RETURN NULL;  -- the expense was deleted with its splits
  END IF;
  SELECT coalesce(sum(s.share_minor), 0) INTO total FROM shared_expense_splits s WHERE s.expense_id = e;
  IF total <> amount THEN
    RAISE EXCEPTION 'the splits of expense % add up to %, not %', e, total, amount USING ERRCODE = 'check_violation';
  END IF;
  RETURN NULL;
END $$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER shared_split_sum AFTER INSERT OR UPDATE OR DELETE ON shared_expense_splits
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION shared_split_sum();
CREATE CONSTRAINT TRIGGER shared_split_sum AFTER INSERT OR UPDATE OF amount_minor ON shared_expenses
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION shared_split_sum();

CREATE TABLE shared_settlements (
  id           uuid PRIMARY KEY,
  group_id     uuid NOT NULL REFERENCES shared_groups(id) ON DELETE CASCADE,
  from_member  uuid NOT NULL,
  to_member    uuid NOT NULL,
  amount_minor bigint NOT NULL CHECK (amount_minor > 0),
  settled_on   date NOT NULL,
  note         text NOT NULL DEFAULT '' CHECK (char_length(note) <= 500),
  created_by   uuid REFERENCES users(id) ON DELETE SET NULL,
  created_at   timestamptz NOT NULL DEFAULT now(),
  CHECK (from_member <> to_member),
  FOREIGN KEY (group_id, from_member) REFERENCES shared_group_members (group_id, id),
  FOREIGN KEY (group_id, to_member) REFERENCES shared_group_members (group_id, id)
);
CREATE INDEX shared_settlements_group_idx ON shared_settlements (group_id, settled_on DESC);
CREATE INDEX shared_settlements_from_idx ON shared_settlements (group_id, from_member);
CREATE INDEX shared_settlements_to_idx ON shared_settlements (group_id, to_member);
CREATE INDEX shared_settlements_created_by_idx ON shared_settlements (created_by);

-- A member's private link from one of their own transactions (keyed like tx_annotations, so it
-- survives delete + re-import) to the copy in a shared group. Only its owner sees it.
CREATE TABLE shared_expense_links (
  user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  account_id   uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  fingerprint  bytea NOT NULL,
  occurrence   integer NOT NULL,
  split_scope  uuid NOT NULL,
  expense_id   uuid NOT NULL REFERENCES shared_expenses(id) ON DELETE CASCADE,
  PRIMARY KEY (account_id, fingerprint, occurrence, split_scope)
);
CREATE INDEX shared_expense_links_user_idx ON shared_expense_links (user_id);
CREATE UNIQUE INDEX shared_expense_links_expense_idx ON shared_expense_links (expense_id);

-- A personal group linked to a shared one: putting a transaction into it shares a copy.
ALTER TABLE groups ADD COLUMN shared_group_id uuid REFERENCES shared_groups(id) ON DELETE SET NULL;
CREATE UNIQUE INDEX groups_shared_group_idx ON groups (user_id, shared_group_id) WHERE shared_group_id IS NOT NULL;
CREATE INDEX groups_shared_group_fk_idx ON groups (shared_group_id);

-- Policies. me = the caller (app.user_id).
ALTER TABLE shared_groups ENABLE ROW LEVEL SECURITY;
ALTER TABLE shared_groups FORCE ROW LEVEL SECURITY;
CREATE POLICY shared_groups_read ON shared_groups FOR SELECT
  USING (owner_id = nullif(current_setting('app.user_id', true), '')::uuid OR shared_member_role(id) IS NOT NULL);
CREATE POLICY shared_groups_create ON shared_groups FOR INSERT
  WITH CHECK (owner_id = nullif(current_setting('app.user_id', true), '')::uuid);
CREATE POLICY shared_groups_change ON shared_groups FOR UPDATE
  USING (shared_member_role(id) = 'owner') WITH CHECK (shared_member_role(id) = 'owner');
CREATE POLICY shared_groups_delete ON shared_groups FOR DELETE
  USING (shared_member_role(id) = 'owner');

ALTER TABLE shared_group_members ENABLE ROW LEVEL SECURITY;  -- not forced: see the header
CREATE POLICY shared_group_members_read ON shared_group_members FOR SELECT
  -- your own row too: the function reads the statement's snapshot, so an INSERT … RETURNING of
  -- the owner's first row (and a member who left) needs it
  USING (user_id = nullif(current_setting('app.user_id', true), '')::uuid OR shared_member_role(group_id) IS NOT NULL);
CREATE POLICY shared_group_members_invite ON shared_group_members FOR INSERT
  WITH CHECK (shared_member_role(group_id) = 'owner'
    OR (user_id = nullif(current_setting('app.user_id', true), '')::uuid AND role = 'owner' AND state = 'active'
        AND EXISTS (SELECT 1 FROM shared_groups g WHERE g.id = group_id
                    AND g.owner_id = nullif(current_setting('app.user_id', true), '')::uuid)));
CREATE POLICY shared_group_members_change ON shared_group_members FOR UPDATE
  USING (user_id = nullif(current_setting('app.user_id', true), '')::uuid OR shared_member_role(group_id) = 'owner')
  WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid OR shared_member_role(group_id) = 'owner');
CREATE POLICY shared_group_members_withdraw ON shared_group_members FOR DELETE
  USING (shared_member_role(group_id) = 'owner' AND state = 'invited');

ALTER TABLE shared_expenses ENABLE ROW LEVEL SECURITY;
ALTER TABLE shared_expenses FORCE ROW LEVEL SECURITY;
CREATE POLICY shared_expenses_read ON shared_expenses FOR SELECT
  USING (shared_member_role(group_id) IN ('owner', 'member'));
CREATE POLICY shared_expenses_add ON shared_expenses FOR INSERT
  WITH CHECK (shared_member_role(group_id) IN ('owner', 'member')
    AND added_by = nullif(current_setting('app.user_id', true), '')::uuid);
CREATE POLICY shared_expenses_change ON shared_expenses FOR UPDATE
  USING (shared_member_role(group_id) IN ('owner', 'member') AND added_by = nullif(current_setting('app.user_id', true), '')::uuid)
  WITH CHECK (shared_member_role(group_id) IN ('owner', 'member') AND added_by = nullif(current_setting('app.user_id', true), '')::uuid);
CREATE POLICY shared_expenses_delete ON shared_expenses FOR DELETE
  USING (shared_member_role(group_id) IN ('owner', 'member') AND added_by = nullif(current_setting('app.user_id', true), '')::uuid);

ALTER TABLE shared_expense_splits ENABLE ROW LEVEL SECURITY;
ALTER TABLE shared_expense_splits FORCE ROW LEVEL SECURITY;
CREATE POLICY shared_expense_splits_read ON shared_expense_splits FOR SELECT
  USING (shared_member_role(group_id) IN ('owner', 'member'));
CREATE POLICY shared_expense_splits_write ON shared_expense_splits FOR ALL
  USING (EXISTS (SELECT 1 FROM shared_expenses e WHERE e.id = expense_id
                 AND e.added_by = nullif(current_setting('app.user_id', true), '')::uuid))
  WITH CHECK (EXISTS (SELECT 1 FROM shared_expenses e WHERE e.id = expense_id AND e.group_id = shared_expense_splits.group_id
                      AND e.added_by = nullif(current_setting('app.user_id', true), '')::uuid));

ALTER TABLE shared_settlements ENABLE ROW LEVEL SECURITY;
ALTER TABLE shared_settlements FORCE ROW LEVEL SECURITY;
CREATE POLICY shared_settlements_read ON shared_settlements FOR SELECT
  USING (shared_member_role(group_id) IN ('owner', 'member'));
CREATE POLICY shared_settlements_add ON shared_settlements FOR INSERT
  WITH CHECK (shared_member_role(group_id) IN ('owner', 'member')
    AND created_by = nullif(current_setting('app.user_id', true), '')::uuid
    AND EXISTS (SELECT 1 FROM shared_group_members m WHERE m.group_id = shared_settlements.group_id
                AND m.id IN (from_member, to_member)
                AND m.user_id = nullif(current_setting('app.user_id', true), '')::uuid));
CREATE POLICY shared_settlements_delete ON shared_settlements FOR DELETE
  USING (shared_member_role(group_id) IN ('owner', 'member') AND created_by = nullif(current_setting('app.user_id', true), '')::uuid);

ALTER TABLE shared_expense_links ENABLE ROW LEVEL SECURITY;
ALTER TABLE shared_expense_links FORCE ROW LEVEL SECURITY;
CREATE POLICY shared_expense_links_tenant ON shared_expense_links
  USING      (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
  WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

-- +goose Down
DROP TABLE shared_expense_links;
DROP TABLE shared_settlements;
DROP TABLE shared_expense_splits;
DROP TABLE shared_expenses;
DROP FUNCTION shared_split_sum();
DROP INDEX groups_shared_group_fk_idx;
DROP INDEX groups_shared_group_idx;
ALTER TABLE groups DROP COLUMN shared_group_id;
DROP TABLE shared_group_members;
DROP FUNCTION shared_member_transition();
DROP TABLE shared_groups;
DROP FUNCTION shared_member_role(uuid);
