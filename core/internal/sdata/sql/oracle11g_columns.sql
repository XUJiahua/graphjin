WITH pk_cols AS (
    SELECT
        acc.owner,
        acc.table_name,
        acc.column_name
    FROM all_constraints ac
    JOIN all_cons_columns acc
      ON ac.owner = acc.owner
     AND ac.constraint_name = acc.constraint_name
    WHERE ac.constraint_type = 'P'
),
uk_cols AS (
    SELECT
        acc.owner,
        acc.table_name,
        acc.column_name
    FROM all_constraints ac
    JOIN all_cons_columns acc
      ON ac.owner = acc.owner
     AND ac.constraint_name = acc.constraint_name
    WHERE ac.constraint_type = 'U'
),
fk_cols AS (
    SELECT
        acc.owner,
        acc.table_name,
        acc.column_name,
        r_ac.owner AS foreignkey_schema,
        r_ac.table_name AS foreignkey_table,
        r_acc.column_name AS foreignkey_column
    FROM all_constraints ac
    JOIN all_cons_columns acc
      ON ac.owner = acc.owner
     AND ac.constraint_name = acc.constraint_name
    JOIN all_constraints r_ac
      ON ac.r_owner = r_ac.owner
     AND ac.r_constraint_name = r_ac.constraint_name
    JOIN all_cons_columns r_acc
      ON r_ac.owner = r_acc.owner
     AND r_ac.constraint_name = r_acc.constraint_name
     AND acc.position = r_acc.position
    WHERE ac.constraint_type = 'R'
),
ctx_cols AS (
    SELECT DISTINCT
        aic.index_owner AS owner,
        aic.table_name,
        aic.column_name
    FROM all_indexes ai
    JOIN all_ind_columns aic
      ON ai.owner = aic.index_owner
     AND ai.index_name = aic.index_name
    WHERE ai.ityp_name = 'CONTEXT'
)
SELECT
    tc.owner AS "schema",
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
FROM all_tab_columns tc
LEFT JOIN pk_cols pk
  ON tc.owner = pk.owner
 AND tc.table_name = pk.table_name
 AND tc.column_name = pk.column_name
LEFT JOIN uk_cols uk
  ON tc.owner = uk.owner
 AND tc.table_name = uk.table_name
 AND tc.column_name = uk.column_name
LEFT JOIN fk_cols fk
  ON tc.owner = fk.owner
 AND tc.table_name = fk.table_name
 AND tc.column_name = fk.column_name
LEFT JOIN ctx_cols ctx
  ON tc.owner = ctx.owner
 AND tc.table_name = ctx.table_name
 AND tc.column_name = ctx.column_name
WHERE tc.owner NOT IN (
    'SYS', 'SYSTEM', 'OUTLN', 'DBSNMP', 'APPQOSSYS', 'XDB', 'WMSYS',
    'CTXSYS', 'MDSYS', 'ORDSYS', 'ORDDATA', 'ORDPLUGINS',
    'SI_INFORMTN_SCHEMA', 'OLAPSYS', 'MDDATA', 'SPATIAL_WFS_ADMIN_USR',
    'SPATIAL_CSW_ADMIN_USR', 'SYSMAN', 'FLOWS_FILES', 'APEX_040200',
    'APEX_PUBLIC_USER', 'LBACSYS', 'DVF', 'DVSYS', 'AUDSYS',
    'GSMADMIN_INTERNAL', 'GSMCATUSER', 'GSMUSER',
    'REMOTE_SCHEDULER_AGENT', 'GGSYS', 'DBSFWUSER', 'ANONYMOUS',
    'XS$NULL', 'OJVMSYS', 'ORACLE_OCM'
)
  AND tc.table_name NOT LIKE 'DR$%'
ORDER BY tc.owner, tc.table_name, tc.column_id
