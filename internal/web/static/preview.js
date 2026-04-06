(function() {
  'use strict';

  var currentAudio = null;
  var currentBtn = null;

  document.addEventListener('click', function(e) {
    var btn = e.target.closest('.preview-btn');
    if (!btn) return;
    var url = btn.getAttribute('data-preview');
    if (!url) return;

    // If clicking the same button that's playing, pause it.
    if (currentBtn === btn && currentAudio && !currentAudio.paused) {
      currentAudio.pause();
      btn.textContent = '\u25B6'; // ▶
      btn.classList.remove('playing');
      currentAudio = null;
      currentBtn = null;
      return;
    }

    // Stop any currently playing preview.
    if (currentAudio) {
      currentAudio.pause();
      if (currentBtn) {
        currentBtn.textContent = '\u25B6';
        currentBtn.classList.remove('playing');
      }
    }

    // Play the new preview.
    var audio = new Audio(url);
    currentAudio = audio;
    currentBtn = btn;
    btn.textContent = '\u23F8'; // ⏸
    btn.classList.add('playing');

    audio.play().catch(function() {
      btn.textContent = '\u25B6';
      btn.classList.remove('playing');
    });

    audio.addEventListener('ended', function() {
      btn.textContent = '\u25B6';
      btn.classList.remove('playing');
      currentAudio = null;
      currentBtn = null;
    });
  });
})();
