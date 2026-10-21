# Notes

The bug was a classic check-then-act: the handler read HasEntry, AwardedInMonth, and Append
as three separate calls — any concurrent replica could slip in between them. The fix moves
the atomicity down into the Store, which in production means inside the Postgres transaction.

## Postgres

Add `UNIQUE (payment_id, kind)` to `ledger_entries` so duplicate inserts are no-ops.

For the award, an advisory lock on (member, month) serialises the cap calculation. `FOR UPDATE`
would only lock existing rows, so the first payment of the month would still race — the advisory
lock covers that case:

    SELECT pg_advisory_xact_lock(hashtext($member_id||$month));
    INSERT INTO ledger_entries(member_id, payment_id, kind, points, month)
    SELECT $1, $3, 'award', LEAST($pts, GREATEST(0, $cap - COALESCE(SUM(points), 0))), $2
    FROM ledger_entries WHERE member_id=$1 AND month=$2 AND kind='award'
    ON CONFLICT (payment_id, kind) DO NOTHING;

For reversals, pull the points straight from the award row rather than recalculating from the
event — that way a capped payment is reversed by the right amount:

    INSERT INTO ledger_entries(member_id, payment_id, kind, points, month)
    SELECT member_id, payment_id, 'reversal', -points, month FROM ledger_entries
    WHERE payment_id=$3 AND kind='award' ON CONFLICT (payment_id, kind) DO NOTHING;

Different members never share a lock. Redeliveries hit the ON CONFLICT and do nothing.
