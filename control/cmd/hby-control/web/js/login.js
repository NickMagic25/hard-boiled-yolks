window.hbyTheme.initControls();

const passwordForm = document.getElementById("passwordForm");
const oidcRow = document.getElementById("oidcRow");

if (document.body.dataset.passwordEnabled !== "true") {
  passwordForm.hidden = true;
}

if (document.body.dataset.oidcEnabled !== "true") {
  oidcRow.hidden = true;
}
