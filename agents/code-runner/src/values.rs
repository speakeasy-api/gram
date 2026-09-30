use anyhow::{Result, bail, ensure};
use monty_types::{MontyObject, ObjectRef};
use serde_json::{Map, Number, Value};
use std::str::FromStr;

const MAX_NUMBER_BITS: u64 = 4096;
const MAX_NUMBER_BYTES: usize = 1236;

// Charge both traversal and strings before allocating the public projection.
// The depth and node budgets also bound cycles and exponentially shared graphs.
pub fn to_json(object: ObjectRef<'_>, max_bytes: usize) -> Result<Value> {
    let mut budget = Budget {
        bytes: max_bytes,
        nodes: 100_000,
    };
    let value = convert(object, 0, &mut budget)?;
    ensure!(
        serde_json::to_vec(&value)?.len() <= max_bytes,
        "JSON result exceeds limit"
    );
    Ok(value)
}

struct Budget {
    bytes: usize,
    nodes: usize,
}

impl Budget {
    fn charge(&mut self, bytes: usize) -> Result<()> {
        self.bytes = self
            .bytes
            .checked_sub(bytes)
            .ok_or_else(|| anyhow::anyhow!("JSON result exceeds limit"))?;
        self.nodes = self
            .nodes
            .checked_sub(1)
            .ok_or_else(|| anyhow::anyhow!("JSON result has too many values"))?;
        Ok(())
    }
}

fn convert(object: ObjectRef<'_>, depth: usize, budget: &mut Budget) -> Result<Value> {
    ensure!(depth <= 64, "JSON value is cyclic or nested too deeply");
    budget.charge(1)?;
    match object.type_name() {
        "NoneType" => Ok(Value::Null),
        "bool" => Ok(Value::Bool(object.as_bool().unwrap())),
        "int" => {
            if let Some(value) = object.as_int() {
                return Ok(Value::Number(value.into()));
            }
            // This accessor is isolated behind the exact Monty revision pin.
            // Bound bits before decimal formatting, outside the worker budget.
            let monty_types::unstable::MontyNode::BigInt(integer) =
                monty_types::unstable::node(object)
            else {
                bail!("unsupported Python integer")
            };
            ensure!(
                integer.bits() <= MAX_NUMBER_BITS,
                "JSON integer exceeds 4096 bits"
            );
            let repr = integer.to_string();
            budget.charge(repr.len())?;
            Ok(Value::Number(Number::from_str(&repr)?))
        }
        "float" => Ok(Value::Number(
            Number::from_f64(object.as_float().unwrap())
                .ok_or_else(|| anyhow::anyhow!("non-finite JSON number"))?,
        )),
        "str" => {
            let value = object.as_str().unwrap();
            budget.charge(value.len())?;
            Ok(Value::String(value.to_owned()))
        }
        "list" | "tuple" => {
            let mut values = Vec::new();
            for value in object.items().unwrap() {
                values.push(convert(value, depth + 1, budget)?);
            }
            Ok(Value::Array(values))
        }
        "dict" => {
            let mut values = Map::new();
            for (key, value) in object.pairs().unwrap() {
                let key = key
                    .as_str()
                    .ok_or_else(|| anyhow::anyhow!("JSON object keys must be strings"))?;
                budget.charge(key.len())?;
                values.insert(key.to_owned(), convert(value, depth + 1, budget)?);
            }
            Ok(Value::Object(values))
        }
        _ => bail!("result must contain only JSON-compatible values"),
    }
}

pub fn from_json(value: Value) -> Result<MontyObject> {
    Ok(match value {
        Value::Null => MontyObject::none(),
        Value::Bool(value) => MontyObject::bool(value),
        Value::Number(value) => {
            let raw = value.to_string();
            ensure!(
                raw.len() <= MAX_NUMBER_BYTES,
                "JSON number exceeds digit limit"
            );
            if let Some(value) = value.as_i64() {
                MontyObject::int(value)
            } else if let Ok(value) = num_bigint::BigInt::from_str(&raw) {
                ensure!(
                    value.bits() <= MAX_NUMBER_BITS,
                    "JSON integer exceeds 4096 bits"
                );
                MontyObject::bigint(value)
            } else {
                let value = value
                    .as_f64()
                    .filter(|v| v.is_finite())
                    .ok_or_else(|| anyhow::anyhow!("unsupported JSON number"))?;
                MontyObject::float(value)
            }
        }
        Value::String(value) => MontyObject::string(value),
        Value::Array(values) => MontyObject::list(
            values
                .into_iter()
                .map(from_json)
                .collect::<Result<Vec<_>>>()?,
        ),
        Value::Object(values) => MontyObject::dict(
            values
                .into_iter()
                .map(|(key, value)| Ok((MontyObject::string(key), from_json(value)?)))
                .collect::<Result<Vec<_>>>()?,
        ),
    })
}
