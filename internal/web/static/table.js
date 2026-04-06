(function() {
  'use strict';

  // --- Row count badge ---
  document.querySelectorAll('.lib-table').forEach(function(table) {
    var tbody = table.querySelector('tbody');
    if (!tbody) return;
    var rows = tbody.querySelectorAll('tr');
    var badge = document.createElement('div');
    badge.className = 'row-count';
    badge.setAttribute('data-total', rows.length);
    badge.textContent = rows.length + (rows.length === 1 ? ' item' : ' items');
    table.parentNode.insertBefore(badge, table);
  });

  // --- Live search/filter ---
  document.querySelectorAll('.lib-search').forEach(function(input) {
    var tableId = input.getAttribute('data-table');
    var table = document.getElementById(tableId);
    if (!table) return;
    var tbody = table.querySelector('tbody');
    var rows = Array.from(tbody.querySelectorAll('tr'));
    var badge = table.parentNode.querySelector('.row-count');
    var total = rows.length;

    input.addEventListener('input', function() {
      var q = input.value.toLowerCase().trim();
      var visible = 0;
      rows.forEach(function(row) {
        var text = row.textContent.toLowerCase();
        var match = !q || text.indexOf(q) !== -1;
        row.style.display = match ? '' : 'none';
        if (match) visible++;
      });
      if (badge) {
        badge.textContent = (q ? visible + ' of ' + total : total) + (total === 1 ? ' item' : ' items');
      }
    });
  });

  // --- Column sorting ---
  document.querySelectorAll('.lib-table th[data-sort]').forEach(function(th) {
    th.style.cursor = 'pointer';
    th.style.userSelect = 'none';
    th.title = 'Click to sort';

    // Add sort indicator
    var arrow = document.createElement('span');
    arrow.className = 'sort-arrow';
    arrow.textContent = ' ↕';
    th.appendChild(arrow);

    th.addEventListener('click', function() {
      var table = th.closest('table');
      var tbody = table.querySelector('tbody');
      var rows = Array.from(tbody.querySelectorAll('tr'));
      var colIdx = Array.from(th.parentNode.children).indexOf(th);
      var type = th.getAttribute('data-sort'); // "text", "num", "date"
      var asc = th.getAttribute('data-dir') !== 'asc';
      th.setAttribute('data-dir', asc ? 'asc' : 'desc');

      // Reset other headers
      table.querySelectorAll('th[data-sort]').forEach(function(h) {
        if (h !== th) {
          h.removeAttribute('data-dir');
          var a = h.querySelector('.sort-arrow');
          if (a) a.textContent = ' ↕';
        }
      });
      arrow.textContent = asc ? ' ↑' : ' ↓';

      rows.sort(function(a, b) {
        var cellA = a.children[colIdx];
        var cellB = b.children[colIdx];
        if (!cellA || !cellB) return 0;
        var va = (cellA.getAttribute('data-val') || cellA.textContent).trim();
        var vb = (cellB.getAttribute('data-val') || cellB.textContent).trim();

        if (type === 'num') {
          va = parseFloat(va) || 0;
          vb = parseFloat(vb) || 0;
          return asc ? va - vb : vb - va;
        }
        if (type === 'date') {
          va = new Date(va) || 0;
          vb = new Date(vb) || 0;
          return asc ? va - vb : vb - va;
        }
        va = va.toLowerCase();
        vb = vb.toLowerCase();
        if (va < vb) return asc ? -1 : 1;
        if (va > vb) return asc ? 1 : -1;
        return 0;
      });

      rows.forEach(function(row) { tbody.appendChild(row); });
    });
  });
})();
