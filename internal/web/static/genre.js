(function() {
  'use strict';

  // 8 colors that rotate based on a hash of the genre name.
  var colors = [
    '#e74c8b', // pink
    '#f59e42', // orange
    '#42b883', // green
    '#6c5ce7', // purple
    '#3498db', // blue
    '#e67e22', // amber
    '#1abc9c', // teal
    '#e84393', // magenta
  ];

  function hashCode(str) {
    var hash = 0;
    for (var i = 0; i < str.length; i++) {
      hash = ((hash << 5) - hash) + str.charCodeAt(i);
      hash |= 0;
    }
    return Math.abs(hash);
  }

  document.querySelectorAll('.genre-pill').forEach(function(pill) {
    var genre = (pill.getAttribute('data-genre') || '').toLowerCase();
    var idx = hashCode(genre) % colors.length;
    var c = colors[idx];
    pill.style.borderColor = c;
    pill.style.color = c;
    pill.style.backgroundColor = c + '18'; // ~10% opacity
  });
})();
