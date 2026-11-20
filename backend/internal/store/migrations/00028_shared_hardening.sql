-- Phase 23 (task 23.10): the database rules of shared groups follow the application's (register
-- rows API-3, API-7, API-8, DB-2, DB-6; decisions of the maintainer on 2026-11-20).
--
--   - Only active members take part: an expense's payer (when it is added or the payer changes)
--     and both sides of a settlement. shared_member_state() reads a member's state for the
--     policies and the trigger.
--   - An invitation is a member row with role 'member' and state 'invited' for a friend of the
--     owner; nothing else can be inserted for someone else.
--   - A member's display name changes only through that member's own row.
--   - Either side of a settlement may remove it, not only whoever recorded it.
--   - When the author of an expense no longer has an account, the group's owner may change or
--     delete it (before, nobody could, and the group could never be settled or deleted).
--   - When a group's owner deletes their account, the group passes to the active member who
--     joined first. Groups that are ownerless already are handed over by this migration.
--
-- shared_groups has FORCE ROW LEVEL SECURITY, which binds the functions' owner too, so the
-- handover updates the group as its new owner (app.user_id for that one statement).

-- +goose Up
CREATE FUNCTION shared_member_state(g uuid, m uuid) RETURNS text
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog, public AS $$
  SELECT x.state FROM public.shared_group_members x WHERE x.group_id = g AND x.id = m
$$;
REVOKE ALL ON FUNCTION shared_member_state(uuid, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION shared_member_state(uuid, uuid) TO finuance_app;

-- Member rows: the role changes only in a handover (run by the table's owner, below); a display
-- name only through the member's own row.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION shared_member_transition() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.group_id <> OLD.group_id OR NEW.id <> OLD.id THEN
    RAISE EXCEPTION 'a membership''s group is fixed' USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.role <> OLD.role
     AND current_user <> (SELECT pg_catalog.pg_get_userbyid(c.relowner) FROM pg_catalog.pg_class c WHERE c.oid = TG_RELID) THEN
    RAISE EXCEPTION 'a membership''s group and role are fixed' USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.user_id IS DISTINCT FROM OLD.user_id AND NEW.user_id IS NOT NULL THEN
    RAISE EXCEPTION 'a membership''s user is fixed' USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.display_name <> OLD.display_name
     AND OLD.user_id IS DISTINCT FROM nullif(current_setting('app.user_id', true), '')::uuid THEN
    RAISE EXCEPTION 'a member''s name changes through their own membership only' USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.state <> OLD.state AND NOT (
       (OLD.state = 'invited' AND NEW.state IN ('active', 'left'))
    OR (OLD.state = 'active' AND NEW.state = 'left')) THEN
    RAISE EXCEPTION 'membership state % cannot become %', OLD.state, NEW.state USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;
-- +goose StatementEnd

DROP POLICY shared_group_members_invite ON shared_group_members;
CREATE POLICY shared_group_members_invite ON shared_group_members FOR INSERT
  WITH CHECK (
    -- the owner invites a friend
    (shared_member_role(group_id) = 'owner' AND role = 'member' AND state = 'invited'
     AND EXISTS (SELECT 1 FROM friendships f
                 WHERE (f.user_a = nullif(current_setting('app.user_id', true), '')::uuid AND f.user_b = shared_group_members.user_id)
                    OR (f.user_b = nullif(current_setting('app.user_id', true), '')::uuid AND f.user_a = shared_group_members.user_id)))
    -- or a new group's owner adds themselves
    OR (user_id = nullif(current_setting('app.user_id', true), '')::uuid AND role = 'owner' AND state = 'active'
        AND EXISTS (SELECT 1 FROM shared_groups g WHERE g.id = group_id
                    AND g.owner_id = nullif(current_setting('app.user_id', true), '')::uuid)));

-- An expense is paid by an active member, when it is added and when the payer changes (an old
-- expense of someone who has left can still be edited otherwise).
-- +goose StatementBegin
CREATE FUNCTION shared_expense_payer() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF (TG_OP = 'INSERT' OR NEW.paid_by <> OLD.paid_by)
     AND shared_member_state(NEW.group_id, NEW.paid_by) IS DISTINCT FROM 'active' THEN
    RAISE EXCEPTION 'the payer of a shared expense must be an active member' USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER shared_expense_payer BEFORE INSERT OR UPDATE OF paid_by ON shared_expenses
  FOR EACH ROW EXECUTE FUNCTION shared_expense_payer();

-- An expense is changed by whoever added it; when that account is gone, by the group's owner.
DROP POLICY shared_expenses_change ON shared_expenses;
CREATE POLICY shared_expenses_change ON shared_expenses FOR UPDATE
  USING (shared_member_role(group_id) IN ('owner', 'member')
    AND (added_by = nullif(current_setting('app.user_id', true), '')::uuid
         OR (added_by IS NULL AND shared_member_role(group_id) = 'owner')))
  WITH CHECK (shared_member_role(group_id) IN ('owner', 'member')
    AND (added_by = nullif(current_setting('app.user_id', true), '')::uuid
         OR (added_by IS NULL AND shared_member_role(group_id) = 'owner')));
DROP POLICY shared_expenses_delete ON shared_expenses;
CREATE POLICY shared_expenses_delete ON shared_expenses FOR DELETE
  USING (shared_member_role(group_id) IN ('owner', 'member')
    AND (added_by = nullif(current_setting('app.user_id', true), '')::uuid
         OR (added_by IS NULL AND shared_member_role(group_id) = 'owner')));
DROP POLICY shared_expense_splits_write ON shared_expense_splits;
CREATE POLICY shared_expense_splits_write ON shared_expense_splits FOR ALL
  USING (EXISTS (SELECT 1 FROM shared_expenses e WHERE e.id = expense_id
                 AND (e.added_by = nullif(current_setting('app.user_id', true), '')::uuid
                      OR (e.added_by IS NULL AND shared_member_role(e.group_id) = 'owner'))))
  WITH CHECK (EXISTS (SELECT 1 FROM shared_expenses e WHERE e.id = expense_id AND e.group_id = shared_expense_splits.group_id
                      AND (e.added_by = nullif(current_setting('app.user_id', true), '')::uuid
                           OR (e.added_by IS NULL AND shared_member_role(e.group_id) = 'owner'))));

-- A settlement is between two active members, one of them the caller; either side may remove it.
DROP POLICY shared_settlements_add ON shared_settlements;
CREATE POLICY shared_settlements_add ON shared_settlements FOR INSERT
  WITH CHECK (shared_member_role(group_id) IN ('owner', 'member')
    AND created_by = nullif(current_setting('app.user_id', true), '')::uuid
    AND shared_member_state(group_id, from_member) = 'active'
    AND shared_member_state(group_id, to_member) = 'active'
    AND EXISTS (SELECT 1 FROM shared_group_members m WHERE m.group_id = shared_settlements.group_id
                AND m.id IN (from_member, to_member)
                AND m.user_id = nullif(current_setting('app.user_id', true), '')::uuid));
DROP POLICY shared_settlements_delete ON shared_settlements;
CREATE POLICY shared_settlements_delete ON shared_settlements FOR DELETE
  USING (shared_member_role(group_id) IN ('owner', 'member')
    AND (created_by = nullif(current_setting('app.user_id', true), '')::uuid
         OR EXISTS (SELECT 1 FROM shared_group_members m WHERE m.group_id = shared_settlements.group_id
                    AND m.id IN (from_member, to_member)
                    AND m.user_id = nullif(current_setting('app.user_id', true), '')::uuid)));

-- Handover: the active member who joined first becomes the owner. Without one the group stays as
-- it is (nobody is left to own it).
-- +goose StatementBegin
CREATE FUNCTION shared_handover(g uuid, leaving uuid) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE
  heir_member uuid;
  heir_user uuid;
  was text := current_setting('app.user_id', true);
BEGIN
  SELECT m.id, m.user_id INTO heir_member, heir_user
  FROM public.shared_group_members m
  WHERE m.group_id = g AND m.state = 'active' AND m.user_id IS NOT NULL AND m.user_id IS DISTINCT FROM leaving
  ORDER BY m.joined_at NULLS LAST, m.created_at, m.id
  LIMIT 1;
  IF heir_member IS NULL THEN
    RETURN;
  END IF;
  UPDATE public.shared_group_members SET role = 'member' WHERE group_id = g AND role = 'owner' AND id <> heir_member;
  UPDATE public.shared_group_members SET role = 'owner' WHERE id = heir_member;
  -- as the new owner: the table's policies bind this function too (FORCE ROW LEVEL SECURITY)
  PERFORM set_config('app.user_id', heir_user::text, true);
  UPDATE public.shared_groups SET owner_id = heir_user WHERE id = g;
  PERFORM set_config('app.user_id', coalesce(was, ''), true);
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION shared_handover(uuid, uuid) FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION shared_handover_on_delete() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE
  g uuid;
BEGIN
  -- the groups this user owns, found through the member rows (readable without the group's policy)
  FOR g IN SELECT m.group_id FROM public.shared_group_members m WHERE m.user_id = OLD.id AND m.role = 'owner' LOOP
    PERFORM public.shared_handover(g, OLD.id);
  END LOOP;
  RETURN OLD;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION shared_handover_on_delete() FROM PUBLIC;
CREATE TRIGGER shared_handover_on_delete BEFORE DELETE ON users
  FOR EACH ROW EXECUTE FUNCTION shared_handover_on_delete();

-- Groups whose owner is gone already.
-- +goose StatementBegin
DO $$
DECLARE
  g uuid;
BEGIN
  FOR g IN SELECT m.group_id FROM shared_group_members m WHERE m.role = 'owner' AND m.user_id IS NULL LOOP
    PERFORM shared_handover(g, NULL);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER shared_handover_on_delete ON users;
DROP FUNCTION shared_handover_on_delete();
DROP FUNCTION shared_handover(uuid, uuid);
DROP POLICY shared_settlements_delete ON shared_settlements;
CREATE POLICY shared_settlements_delete ON shared_settlements FOR DELETE
  USING (shared_member_role(group_id) IN ('owner', 'member') AND created_by = nullif(current_setting('app.user_id', true), '')::uuid);
DROP POLICY shared_settlements_add ON shared_settlements;
CREATE POLICY shared_settlements_add ON shared_settlements FOR INSERT
  WITH CHECK (shared_member_role(group_id) IN ('owner', 'member')
    AND created_by = nullif(current_setting('app.user_id', true), '')::uuid
    AND EXISTS (SELECT 1 FROM shared_group_members m WHERE m.group_id = shared_settlements.group_id
                AND m.id IN (from_member, to_member)
                AND m.user_id = nullif(current_setting('app.user_id', true), '')::uuid));
DROP POLICY shared_expense_splits_write ON shared_expense_splits;
CREATE POLICY shared_expense_splits_write ON shared_expense_splits FOR ALL
  USING (EXISTS (SELECT 1 FROM shared_expenses e WHERE e.id = expense_id
                 AND e.added_by = nullif(current_setting('app.user_id', true), '')::uuid))
  WITH CHECK (EXISTS (SELECT 1 FROM shared_expenses e WHERE e.id = expense_id AND e.group_id = shared_expense_splits.group_id
                      AND e.added_by = nullif(current_setting('app.user_id', true), '')::uuid));
DROP POLICY shared_expenses_delete ON shared_expenses;
CREATE POLICY shared_expenses_delete ON shared_expenses FOR DELETE
  USING (shared_member_role(group_id) IN ('owner', 'member') AND added_by = nullif(current_setting('app.user_id', true), '')::uuid);
DROP POLICY shared_expenses_change ON shared_expenses;
CREATE POLICY shared_expenses_change ON shared_expenses FOR UPDATE
  USING (shared_member_role(group_id) IN ('owner', 'member') AND added_by = nullif(current_setting('app.user_id', true), '')::uuid)
  WITH CHECK (shared_member_role(group_id) IN ('owner', 'member') AND added_by = nullif(current_setting('app.user_id', true), '')::uuid);
DROP TRIGGER shared_expense_payer ON shared_expenses;
DROP FUNCTION shared_expense_payer();
DROP POLICY shared_group_members_invite ON shared_group_members;
CREATE POLICY shared_group_members_invite ON shared_group_members FOR INSERT
  WITH CHECK (shared_member_role(group_id) = 'owner'
    OR (user_id = nullif(current_setting('app.user_id', true), '')::uuid AND role = 'owner' AND state = 'active'
        AND EXISTS (SELECT 1 FROM shared_groups g WHERE g.id = group_id
                    AND g.owner_id = nullif(current_setting('app.user_id', true), '')::uuid)));
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION shared_member_transition() RETURNS trigger
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
DROP FUNCTION shared_member_state(uuid, uuid);
