-- anchor_date preserves the schedule's intended day-of-month/year across
-- month-end clamping: a rule anchored on Jan 31 is due Feb 28 (clamped) and
-- then Mar 31 again. next_due_date alone loses the 31 after the first clamp.
ALTER TABLE recurring_rules ADD COLUMN anchor_date date;
UPDATE recurring_rules SET anchor_date = next_due_date;
ALTER TABLE recurring_rules ALTER COLUMN anchor_date SET NOT NULL;
