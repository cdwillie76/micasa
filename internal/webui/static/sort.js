// Copyright 2026 Phillip Cloud
// Licensed under the Apache License, Version 2.0
//
// Click-to-sort for <table class="sortable">. No dependencies, no build
// step -- consistent with the rest of this UI. Numeric-looking cell text
// (money, counts, plain numbers) sorts numerically; everything else sorts
// as a case-insensitive string. Progressive enhancement only: tables work
// fine, just unsorted, with JS disabled.
(() => {
  function cellValue(row, index) {
    var cell = row.children[index];
    return cell ? cell.textContent.trim() : "";
  }

  // byteUnits maps humanize.IBytes suffixes to their power-of-1024
  // multiplier, so "1.2 MiB" sorts after "900 KiB" instead of comparing
  // 1.2 to 900 as if they were the same unit.
  var byteUnits = { b: 0, kib: 1, mib: 2, gib: 3, tib: 4, pib: 5 };

  // numericValue returns a number only when the ENTIRE cell is numeric --
  // never a prefix match. A prefix match would silently misparse an ISO
  // date like "2026-08-16" as the number 2026, breaking its sort order
  // against other dates in the same year. Values that don't fully match
  // fall through to string comparison, where ISO dates already sort
  // correctly on their own.
  function numericValue(text) {
    var trimmed = text.trim();
    var byteMatch = trimmed.match(/^(-?[\d.]+)\s*(b|kib|mib|gib|tib|pib)$/i);
    var amount = byteMatch ? parseFloat(byteMatch[1]) : null;

    if (byteMatch) {
      return Number.isNaN(amount)
        ? null
        : amount * 1024 ** byteUnits[byteMatch[2].toLowerCase()];
    }

    // Otherwise strip "$", "," and one trailing unit suffix, assuming the
    // unit is the same for every row in the column (true for money,
    // interval-in-months, area, and bed/bath counts as currently rendered).
    var cleaned = trimmed
      .replace(/^\$/, "")
      .replace(/,/g, "")
      .replace(/\s*(k|mo|ft²|m²|ba|bd|%)$/i, "");
    if (!/^-?\d+(\.\d+)?$/.test(cleaned)) return null;
    var n = parseFloat(cleaned);
    return Number.isNaN(n) ? null : n;
  }

  function sortTable(table, index, ascending) {
    var tbody = table.tBodies[0];
    if (!tbody) return;
    var rows = Array.prototype.slice.call(tbody.rows);

    rows.sort((a, b) => {
      var av = cellValue(a, index);
      var bv = cellValue(b, index);
      var an = numericValue(av);
      var bn = numericValue(bv);
      var cmp;
      if (an !== null && bn !== null) {
        cmp = an - bn;
      } else {
        cmp = av.toLowerCase().localeCompare(bv.toLowerCase());
      }
      return ascending ? cmp : -cmp;
    });

    rows.forEach((row) => {
      tbody.appendChild(row);
    });
  }

  document.querySelectorAll("table.sortable").forEach((table) => {
    var headerRow = table.tHead?.rows[0];
    if (!headerRow) return;

    Array.prototype.forEach.call(headerRow.cells, (th, index) => {
      if (th.dataset.noSort !== undefined || th.textContent.trim() === "") return;
      th.classList.add("sort-header");
      th.tabIndex = 0;

      var activate = () => {
        var ascending = th.dataset.sortDir !== "asc";
        Array.prototype.forEach.call(headerRow.cells, (other) => {
          delete other.dataset.sortDir;
        });
        th.dataset.sortDir = ascending ? "asc" : "desc";
        sortTable(table, index, ascending);
      };

      th.addEventListener("click", activate);
      th.addEventListener("keydown", (e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          activate();
        }
      });
    });
  });
})();
