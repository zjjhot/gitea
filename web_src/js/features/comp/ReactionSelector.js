import $ from 'jquery';
import {POST} from '../../modules/fetch.js';

export function initCompReactionSelector() {
  for (const container of document.querySelectorAll('.issue-content, .diff-file-body')) {
    container.addEventListener('click', async (e) => {
      // there are 2 places for the "reaction" buttons, one is the top-right reaction menu, one is the bottom of the comment
      const target = e.target.closest('.comment-reaction-button');
      if (!target) return;
      e.preventDefault();

      if (target.classList.contains('disabled')) return;

      const actionUrl = target.closest('[data-action-url]').getAttribute('data-action-url');
      const reactionContent = target.getAttribute('data-reaction-content');

      const commentContainer = target.closest('.comment-container');

      const bottomReactions = commentContainer.querySelector('.bottom-reactions'); // may not exist if there is no reaction
      const bottomReactionBtn = bottomReactions?.querySelector(`a[data-reaction-content="${CSS.escape(reactionContent)}"]`);
      const hasReacted = bottomReactionBtn?.getAttribute('data-has-reacted') === 'true';

      const res = await POST(`${actionUrl}/${hasReacted ? 'unreact' : 'react'}`, {
        data: new URLSearchParams({content: reactionContent}),
      });

      const data = await res.json();
      bottomReactions?.remove();
      if (data.html) {
        commentContainer.insertAdjacentHTML('beforeend', data.html);
        const bottomReactionsDropdowns = commentContainer.querySelectorAll('.bottom-reactions .dropdown.select-reaction');
        $(bottomReactionsDropdowns).dropdown(); // re-init the dropdown
      }
    });
  }
}
