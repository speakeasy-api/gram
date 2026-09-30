-- Disposable benchmark database only. Seed through the raw source and all MVs.
-- Run 300 batches per instrument (offset=0,10000,...2990000, records=10000).
-- Parameters: instrument=counter|histogram, metric=requests|duration (optionally
-- suffixed _stable), series_values=1000 (wide churn) or 8 (bounded dimensions),
-- end=<fixed UTC Unix seconds>, days=90. Keep end fixed for the complete seed.
SET async_insert=0, max_threads=2, max_memory_usage=600000000;
INSERT INTO product_metric_contributions
 (organization_id, project_id, metric_name, scope_name, scope_version, unit, instrument, number_kind,
  resource_attributes, scope_attributes, point_attributes, contribution_id, event_time, observed_at, integer_value, floating_value)
SELECT if(cityHash64(number,1)%10=0,'synthetic-small','synthetic-large'),
 toUUID('00000000-0000-4000-8000-000000000001'), concat('gram.synthetic.',{metric:String}), 'gram.synthetic','1',
 if({instrument:String}='counter','{request}','s'), {instrument:String}, if({instrument:String}='counter','integer','floating'),
 [('cloud.region','STRING',toJSONString(concat('region-',toString(cityHash64(number,2)%8)))),
  ('deployment.environment.name','STRING',toJSONString(if(cityHash64(number,3)%5=0,'staging','production')))],
 [('library','STRING','"synthetic"')],
 arraySort(a -> a.1, arrayConcat(
  [('model','STRING',toJSONString(concat('model-',toString(cityHash64(number,4)%4)))),
   ('status','INT64',if(cityHash64(number,5)%10=0,'500','200')),
   ('series','STRING',toJSONString(concat('series-',toString(cityHash64(number,6)%{series_values:UInt64}))))],
  arrayMap(i -> tuple(concat('dimension.',toString(i)),'STRING','"synthetic-value"'),range(17)))),
 concat({metric:String},'-',toString(number)), toDateTime({end:UInt32})-toIntervalMinute(1+cityHash64(number,7)%({days:UInt32}*1440-2)),
 now64(9), if({instrument:String}='counter',1,0), if({instrument:String}='histogram',(1+number%1000)/1000.,0.)
FROM numbers({offset:UInt64},{records:UInt64});
