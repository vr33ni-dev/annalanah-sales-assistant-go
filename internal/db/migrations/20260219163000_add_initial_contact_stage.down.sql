ALTER TABLE IF EXISTS sales_process DROP CONSTRAINT IF EXISTS sales_process_stage_check;
ALTER TABLE IF EXISTS sales_process
  ADD CONSTRAINT sales_process_stage_check CHECK (stage IN ('initial_call_scheduled', 'follow_up','closed','lost'));

ALTER TABLE IF EXISTS clients DROP CONSTRAINT IF EXISTS clients_status_check;
ALTER TABLE IF EXISTS clients
  ADD CONSTRAINT clients_status_check CHECK (status IN ('active','initial_call_scheduled','follow_up_scheduled','awaiting_response','lost','inactive'));
