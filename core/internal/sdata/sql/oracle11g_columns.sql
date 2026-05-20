-- oracle11g_columns.sql
--
-- Uses user_* dictionary views (user_tab_columns, user_constraints,
-- user_cons_columns, user_indexes, user_ind_columns). These views are
-- implicitly scoped to the current session owner and carry no Oracle
-- security-predicate overhead, unlike the all_* equivalents which must
-- evaluate privileges row-by-row across every visible schema.
--
-- This is the Oracle 11g variant: avoids 12c+ features like
-- search_condition_vc and JSON IS JSON checks.
WITH pk_cols AS (
    SELECT
        ucc.table_name,
        ucc.column_name
    FROM user_constraints uc
    JOIN user_cons_columns ucc
      ON uc.constraint_name = ucc.constraint_name
    WHERE uc.constraint_type = 'P'
),
uk_cols AS (
    SELECT DISTINCT
        ucc.table_name,
        ucc.column_name
    FROM user_constraints uc
    JOIN user_cons_columns ucc
      ON uc.constraint_name = ucc.constraint_name
    WHERE uc.constraint_type = 'U'
),
fk_cols AS (
    SELECT
        ucc.table_name,
        ucc.column_name,
        LOWER(r_ac.owner) AS foreignkey_schema,
        LOWER(r_ac.table_name) AS foreignkey_table,
        r_acc.column_name AS foreignkey_column,
        ROW_NUMBER() OVER (
            PARTITION BY ucc.table_name, ucc.column_name
            ORDER BY uc.constraint_name
        ) AS rn
    FROM user_constraints uc
    JOIN user_cons_columns ucc
      ON uc.constraint_name = ucc.constraint_name
    JOIN all_constraints r_ac
      ON uc.r_constraint_name = r_ac.constraint_name
     AND uc.r_owner = r_ac.owner
    JOIN all_cons_columns r_acc
      ON r_ac.owner = r_acc.owner
     AND r_ac.constraint_name = r_acc.constraint_name
     AND ucc.position = r_acc.position
    WHERE uc.constraint_type = 'R'
),
ctx_cols AS (
    SELECT DISTINCT
        uic.table_name,
        uic.column_name
    FROM user_indexes ui
    JOIN user_ind_columns uic
      ON ui.index_name = uic.index_name
    WHERE ui.ityp_name = 'CONTEXT'
)
SELECT
    USER AS "schema",
    tc.table_name AS "table",
    tc.column_name AS "column",
    tc.data_type AS "type",
    CASE WHEN tc.nullable = 'N' THEN 1 ELSE 0 END AS not_null,
    CASE WHEN pk.column_name IS NOT NULL THEN 1 ELSE 0 END AS primary_key,
    CASE WHEN pk.column_name IS NOT NULL OR uk.column_name IS NOT NULL THEN 1 ELSE 0 END AS unique_key,
    CASE
        WHEN tc.data_type = 'CLOB' AND (
            LOWER(tc.column_name) LIKE '%_ids' OR
            LOWER(tc.column_name) = 'tags'
        ) THEN 1
        ELSE 0
    END AS is_array,
    CASE WHEN ctx.column_name IS NOT NULL THEN 1 ELSE 0 END AS full_text,
    NVL(fk.foreignkey_schema, ' ') AS foreignkey_schema,
    NVL(fk.foreignkey_table, ' ') AS foreignkey_table,
    NVL(fk.foreignkey_column, ' ') AS foreignkey_column
FROM user_tab_columns tc
LEFT JOIN pk_cols pk
  ON tc.table_name = pk.table_name
 AND tc.column_name = pk.column_name
LEFT JOIN uk_cols uk
  ON tc.table_name = uk.table_name
 AND tc.column_name = uk.column_name
LEFT JOIN fk_cols fk
  ON tc.table_name = fk.table_name
 AND tc.column_name = fk.column_name
 AND fk.rn = 1
LEFT JOIN ctx_cols ctx
  ON tc.table_name = ctx.table_name
 AND tc.column_name = ctx.column_name
WHERE tc.table_name NOT LIKE 'DR$%'
  AND tc.table_name NOT LIKE 'APEX_%'
  AND tc.table_name NOT LIKE 'WWV_FLOW_%'
ORDER BY tc.table_name, tc.column_id
