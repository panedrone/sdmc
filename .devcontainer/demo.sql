-- The demo schema of the dev container: database postgres, the first start of the db service runs it.

create table groups
(
    g_id   serial primary key,
    g_name varchar(256) not null
);

create table tasks
(
    t_id       serial primary key,
    g_id       integer      not null references groups (g_id) on delete cascade,
    t_priority integer      not null default 1,
    t_date     timestamptz  not null default now(),
    t_subject  varchar(256) not null,
    t_comments text
);

create index ix_tasks_g_id on tasks (g_id);

-- a function, for <query ref="..."/> with your own SQL: select * from tasks_by_priority(?)
create function tasks_by_priority(p_min integer)
    returns setof tasks
    language sql
as
$$
select *
from tasks
where t_priority >= p_min
order by t_priority desc, t_id
$$;

insert into groups (g_name)
values ('home'),
       ('work');

insert into tasks (g_id, t_priority, t_subject, t_comments)
values (1, 1, 'buy milk', null),
       (1, 3, 'fix the tap', 'the kitchen one'),
       (2, 5, 'release sdmc', 'tag v1.0.10'),
       (2, 2, 'answer the issues', null);
