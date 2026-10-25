select b.*, 
(select count(*) from tasks t where b.b_id = t.b_id) as b_tasks_count
from boards b
order by b.b_dt_created, b.b_id