document.addEventListener("DOMContentLoaded", function(){
  document.addEventListener("keydown", function(e) {
    var nav = (window.NAVIGATION || {});
    var key = e.which || e.keyCode;
    switch(key) {
      case 37:
        if (nav.up) {
          window.location = nav.up
        }
        break;
      case 38:
        if (nav.left) {
          window.location = nav.left;
        }
        break;
      case 39:
        if (nav.right) {
          window.location = nav.right;
        }
        break;
      case 40:
        if (nav.down) {
          window.location = nav.down;
        }
        break;
      default:
        return true;
    }

    e.preventDefault();
    e.stopPropagation();
    return false;
  });
});

