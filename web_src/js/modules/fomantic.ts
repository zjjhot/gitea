import $ from 'jquery';
import {initFomanticApiPatch} from './fomantic/api.ts';
import {initAriaCheckboxPatch} from './fomantic/checkbox.ts';
import {initAriaFormFieldPatch} from './fomantic/form.ts';
import {initAriaDropdownPatch} from './fomantic/dropdown.ts';
import {initAriaModalPatch} from './fomantic/modal.ts';
import {initFomanticTransition} from './fomantic/transition.ts';
import {initFomanticDimmer} from './fomantic/dimmer.ts';
import {svg} from '../svg.ts';

export const fomanticMobileScreen = window.matchMedia('only screen and (max-width: 767.98px)');

export function initGiteaFomantic() {
  // Silence fomantic's error logging when tabs are used without a target content element
  $.fn.tab.settings.silent = true;

  // By default, use "exact match" for full text search
  $.fn.dropdown.settings.fullTextSearch = 'exact';
  // Do not use "cursor: pointer" for dropdown labels
  $.fn.dropdown.settings.className.label += ' tw-cursor-default';
  // Always use Gitea's SVG icons
  $.fn.dropdown.settings.templates.label = function(_value, text, preserveHTML, className) {
    const escape = $.fn.dropdown.settings.templates.escape;
    return escape(text, preserveHTML) + svg('octicon-x', 16, `${className.delete} icon`);
  };

  initFomanticTransition();
  initFomanticDimmer();
  initFomanticApiPatch();

  // Use the patches to improve accessibility, these patches are designed to be as independent as possible, make it easy to modify or remove in the future.
  initAriaCheckboxPatch();
  initAriaFormFieldPatch();
  initAriaDropdownPatch();
  initAriaModalPatch();
}
