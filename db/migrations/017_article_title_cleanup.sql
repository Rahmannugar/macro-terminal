-- One-time cleanup: several feeds shipped titles wrapped in markup or
-- HTML entities; ingestion now strips them on every pass.

UPDATE articles
SET title = btrim(
        regexp_replace(
            regexp_replace(
                replace(replace(replace(replace(replace(replace(replace(title,
                    '&lt;', '<'),
                    '&gt;', '>'),
                    '&quot;', '"'),
                    '&#34;', '"'),
                    '&#39;', ''''),
                    '&nbsp;', ' '),
                    '&amp;', '&'),
                '</?[a-zA-Z!?][^>]*>', ' ', 'g'),
            '\s+', ' ', 'g')
    )
WHERE title ~ '</?[a-zA-Z!?][^>]*>|&(amp|quot|nbsp|lt|gt|#34|#39);';

---- create above / drop below ----

-- The stripped markup cannot be reconstructed, and ingestion
-- re-normalizes titles on every refetch anyway.
