// TODO: Move file to better home

import React, { useEffect, useReducer } from "react";
import { formatDistance } from "date-fns";

export const HumanizeDateTime = React.memo(
  ({
    date,
    includeTime = true,
  }: {
    date: Date;
    includeTime?: boolean;
  }): string => {
    const forceRender = useReducer((s) => !s, false)[1];

    useEffect(() => {
      // While the date is less than a minute old, re-render every second
      // While the date is less than an hour old, re-render every minute
      const diff = (Date.now() - date.getTime()) / 1000;
      const delay = diff < 60 ? 1 : diff < 3600 ? 60 : 3600;
      const timeout = setTimeout(() => forceRender(), delay * 1000);
      return () => clearTimeout(timeout);
    });

    // TODO: Re-render if we tick over to a new day
    // TODO: Re-render if we change timezone
    // TODO: Keep global instance of now time up to date and use as reference
    return dateTimeFormatters.humanize(date, { includeTime });
  },
);

export const dateTimeFormatters = {
  humanize: function humanize(
    date: Date,
    {
      referenceDate = new Date(),
      includeSuffix = true,
      includeTime = true,
    } = {},
  ) {
    const delta = referenceDate.valueOf() - date.valueOf();
    const suffix = includeSuffix ? "ago" : "";
    // If less than 12 hours: show formatted distance
    if (delta < 12 * 60 * 60 * 1000) {
      const distance = formatDistance(date, referenceDate);
      return `${distance} ${suffix}`.trim();
    }
    // If today: show "Today, {{localeTimeFormat}}"
    if (date.toDateString() === referenceDate.toDateString()) {
      return (
        "Today" +
        (includeTime ? `, ${dateTimeFormatters.time.format(date)}` : "")
      );
    }
    // If yesterday: show "Yesterday, {{localeTimeFormat}}"
    if (
      date.toDateString() ===
      new Date(referenceDate.valueOf() - 24 * 60 * 60 * 1000).toDateString()
    ) {
      return (
        "Yesterday" +
        (includeTime ? `, ${dateTimeFormatters.time.format(date)}` : "")
      );
    }

    // If same year: display in full without year
    if (date.getFullYear() === referenceDate.getFullYear()) {
      return includeTime
        ? `${dateTimeFormatters.sameYear.format(date)}`
        : `${dateTimeFormatters.monthDay.format(date)}`;
    }

    // Full date
    return `${dateTimeFormatters.full.format(date)}`;
  },
  full: new Intl.DateTimeFormat(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "numeric",
  }),
  sameYear: new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "numeric",
  }),
  monthDay: new Intl.DateTimeFormat(undefined, {
    month: "long",
    day: "numeric",
  }),
  time: new Intl.DateTimeFormat(undefined, {
    hour: "numeric",
    minute: "numeric",
  }),
  logTimestamp: new Intl.DateTimeFormat(undefined, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  }),
};

export const formatDuration = (ms: number) => {
  if (ms < 1000) {
    return `${ms.toFixed(0)}ms`;
  }
  return `${(ms / 1000).toFixed(1)}s`;
};
