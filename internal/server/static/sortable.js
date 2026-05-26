// Tiny table sorter. Click a <th data-col="N"> to sort by that column.
// Detects numeric columns by attempting parseFloat on every cell; falls back
// to lexical comparison.
(function () {
  function attach(table) {
    if (table.__sortable) return;
    table.__sortable = true;
    var headers = table.querySelectorAll("thead th");
    headers.forEach(function (th, idx) {
      th.addEventListener("click", function () {
        sortBy(table, idx, th);
      });
    });
  }

  function sortBy(table, col, header) {
    var dir = header.getAttribute("data-dir") === "asc" ? "desc" : "asc";
    var tbody = table.querySelector("tbody");
    var rows = Array.prototype.slice.call(tbody.querySelectorAll("tr"));

    var values = rows.map(function (r) {
      var c = r.children[col];
      return c ? c.textContent : "";
    });
    var numeric = values.every(function (v) {
      if (v === "") return true;
      return !isNaN(parseFloat(v)) && isFinite(v);
    });

    rows.sort(function (a, b) {
      var av = a.children[col] ? a.children[col].textContent : "";
      var bv = b.children[col] ? b.children[col].textContent : "";
      var cmp;
      if (numeric) {
        cmp = parseFloat(av || 0) - parseFloat(bv || 0);
      } else {
        cmp = av < bv ? -1 : av > bv ? 1 : 0;
      }
      return dir === "asc" ? cmp : -cmp;
    });

    rows.forEach(function (r) {
      tbody.appendChild(r);
    });

    table.querySelectorAll("thead th").forEach(function (h) {
      h.removeAttribute("data-dir");
    });
    header.setAttribute("data-dir", dir);
  }

  function scan(root) {
    (root || document).querySelectorAll("table.output-table").forEach(attach);
  }

  document.addEventListener("DOMContentLoaded", function () { scan(document); });
  document.body && scan(document.body);

  // Re-scan after HTMX swaps in new content.
  document.addEventListener("htmx:afterSwap", function (e) { scan(e.target); });
})();
