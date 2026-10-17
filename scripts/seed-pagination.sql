\set ON_ERROR_STOP on
\if :{?user_id}
\else
  \echo 'Pass -v user_id=EXISTING_USER_ID'
  \quit 1
\endif
\if :{?spent_at}
\else
  \echo 'Pass -v spent_at=YYYY-MM-DD'
  \quit 1
\endif

BEGIN;
SELECT pg_advisory_xact_lock(170102026);
WITH samples AS (
    SELECT sample_number,
           '[pagination-test-v1] ' || lpad(sample_number::text, 3, '0') AS comment
    FROM generate_series(1, 240) AS sample_number
)
INSERT INTO expenses (user_id, amount, category, spent_at, comment)
SELECT :'user_id'::bigint,
       (100 + sample_number * 37 % 1900)::numeric / 100,
       (ARRAY['еда', 'транспорт', 'жильё', 'развлечения', 'здоровье', 'покупки', 'другое'])[1 + (sample_number - 1) % 7],
       :'spent_at'::date,
       samples.comment
FROM samples
WHERE NOT EXISTS (
    SELECT 1 FROM expenses
    WHERE user_id = :'user_id'::bigint AND comment = samples.comment
);
COMMIT;

SELECT users.display_name, count(*) AS test_expenses, sum(expenses.amount) AS test_amount
FROM expenses JOIN users ON users.id = expenses.user_id
WHERE user_id = :'user_id'::bigint AND comment ~ '^\[pagination-test-v1\] [0-9]{3}$'
GROUP BY users.display_name;
