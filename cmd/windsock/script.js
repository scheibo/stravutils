'use strict';

document.addEventListener("DOMContentLoaded", function(){
  var nav = (window.NAVIGATION || {});

  document.addEventListener("keydown", function(e) {
    var key = e.which || e.keyCode;
    switch(key) {
      case 37:
        if (nav.left) {
          window.location = maybeIncludeReload(nav.left);
        }
        break;
      case 38:
        if (nav.up) {
          window.location = maybeIncludeReload(nav.up)
        }
        break;
      case 39:
        if (nav.right) {
          window.location = maybeIncludeReload(nav.right);
        }
        break;
      case 40:
        if (nav.down) {
          window.location = maybeIncludeReload(nav.down);
        }
        break;
      default:
        return true;
    }

    e.preventDefault();
    e.stopPropagation();
    return false;
  });

  /*! pure-swipe.js - v1.0.7, John Doherty <www.johndoherty.info>, MIT License */
  (function (window, document) {
    // patch CustomEvent to allow constructor creation (IE/Chrome) - resolved once initCustomEvent no longer exists
    if ('initCustomEvent' in document.createEvent('CustomEvent')) {

        window.CustomEvent = function (event, params) {

            params = params || { bubbles: false, cancelable: false, detail: undefined };

            var evt = document.createEvent('CustomEvent');
            evt.initCustomEvent(event, params.bubbles, params.cancelable, params.detail);
            return evt;
        };

        window.CustomEvent.prototype = window.Event.prototype;
    }

    document.addEventListener('touchstart', handleTouchStart, false);
    document.addEventListener('touchmove', handleTouchMove, false);
    document.addEventListener('touchend', handleTouchEnd, false);

    var xDown = null;
    var yDown = null;
    var xDiff = null;
    var yDiff = null;
    var timeDown = null;
    var startEl = null;

    function handleTouchEnd(e) {
        // if the user released on a different target, cancel!
        if (startEl !== e.target) return;

        var swipeThreshold = parseInt(startEl.getAttribute('data-swipe-threshold') || '200', 10);
        var swipeTimeout = parseInt(startEl.getAttribute('data-swipe-timeout') || '500', 10);
        var timeDiff = Date.now() - timeDown;
        var eventType = '';

        if (Math.abs(xDiff) > Math.abs(yDiff)) { // most significant
            if (Math.abs(xDiff) > swipeThreshold && timeDiff < swipeTimeout) {
                if (xDiff > 0) {
                    eventType = 'swiped-left';
                }
                else {
                    eventType = 'swiped-right';
                }
            }
        }
        else {
            if (Math.abs(yDiff) > swipeThreshold && timeDiff < swipeTimeout) {
                if (yDiff > 0) {
                    eventType = 'swiped-up';
                }
                else {
                    eventType = 'swiped-down';
                }
            }
        }

        if (eventType !== '') {
            // fire event on the element that started the swipe
            startEl.dispatchEvent(new CustomEvent(eventType, { bubbles: true, cancelable: true }));
        }

        // reset values
        xDown = null;
        yDown = null;
        timeDown = null;
    }

    function handleTouchStart(e) {
        startEl = e.target;

        timeDown = Date.now();
        xDown = e.touches[0].clientX;
        yDown = e.touches[0].clientY;
        xDiff = 0;
        yDiff = 0;
    }

    function handleTouchMove(e) {
        if (!xDown || !yDown) return;

        var xUp = e.touches[0].clientX;
        var yUp = e.touches[0].clientY;

        xDiff = xDown - xUp;
        yDiff = yDown - yUp;
    }
  }(window, document));

	// swiped-up/swiped-down conflicts with PDTR and scrolling. Instead, convert
	// L/R to U/D - for climbs U/D == L/R anyway, but for times U/D is more
	// important because it at least will allow for paging through the whole dataset.
	// NOTE: swiped-left = means go in the R direction, which then gets mapped to D.
  if (nav.down) {
    document.addEventListener("swiped-left", function(e) {
      window.location = maybeIncludeReload(nav.down);
    });
  }
  if (nav.up) {
    document.addEventListener("swiped-right", function(e) {
      window.location = maybeIncludeReload(nav.up);
    });
  }
});

