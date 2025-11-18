import { useState, useEffect, useCallback } from "react";

export function useLocalStorageState<T>(key: string, defaultValue: T) {
  const readValue = useCallback((): T => {
    try {
      const item = window.localStorage.getItem(key);
      return item !== null ? (JSON.parse(item) as T) : defaultValue;
    } catch (err) {
      console.warn(`useLocalStorageState: Error reading key "${key}"`, err);
      return defaultValue;
    }
  }, [key, defaultValue]);

  const [value, setValue] = useState<T>(readValue);

  const setStoredValue = useCallback(
    (val: T | ((prev: T) => T)) => {
      setValue((prev) => {
        const newValue =
          typeof val === "function" ? (val as (p: T) => T)(prev) : val;
        try {
          window.localStorage.setItem(key, JSON.stringify(newValue));
        } catch (err) {
          console.warn(`useLocalStorageState: Error setting key "${key}"`, err);
        }
        return newValue;
      });
    },
    [key],
  );

  // Sync changes across browser tabs
  useEffect(() => {
    const onStorage = (event: StorageEvent) => {
      if (event.key === key) {
        setValue(
          event.newValue ? (JSON.parse(event.newValue) as T) : defaultValue,
        );
      }
    };
    window.addEventListener("storage", onStorage);
    return () => window.removeEventListener("storage", onStorage);
  }, [key, defaultValue]);

  return [value, setStoredValue] as const;
}
